/*
Copyright 2022 Cortex Labs, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"fmt"
	"time"

	"github.com/PEAT-AI/yaml"
	"github.com/cortexlabs/cortex/cli/cluster"
	"github.com/cortexlabs/cortex/cli/types/cliconfig"
	"github.com/cortexlabs/cortex/cli/types/flags"
	"github.com/cortexlabs/cortex/pkg/lib/console"
	libjson "github.com/cortexlabs/cortex/pkg/lib/json"
	"github.com/cortexlabs/cortex/pkg/lib/pointer"
	"github.com/cortexlabs/cortex/pkg/lib/table"
	libtime "github.com/cortexlabs/cortex/pkg/lib/time"
	"github.com/cortexlabs/cortex/pkg/operator/schema"
)

const (
	_titleTaskAPI         = "task api"
	_titleTaskJobCount    = "running jobs"
	_titleLatestTaskJobID = "latest job id"
)

func taskAPIsTable(taskAPIs []schema.APIResponse, envNames []string) table.Table {
	rows := make([][]interface{}, 0, len(taskAPIs))

	for i, taskAPI := range taskAPIs {
		if taskAPI.Metadata == nil {
			continue
		}
		lastAPIUpdated := time.Unix(taskAPI.Metadata.LastUpdated, 0)
		latestStartTime := time.Time{}
		latestJobID := "-"
		runningJobs := 0

		for _, job := range taskAPI.TaskJobStatuses {
			if job.StartTime.After(latestStartTime) {
				latestStartTime = job.StartTime
				latestJobID = job.ID + fmt.Sprintf(" (submitted %s ago)", libtime.SinceStr(&latestStartTime))
			}

			if job.Status.IsInProgress() {
				runningJobs++
			}
		}

		rows = append(rows, []interface{}{
			envNames[i],
			taskAPI.Metadata.Name,
			runningJobs,
			latestJobID,
			libtime.SinceStr(&lastAPIUpdated),
		})
	}

	return table.Table{
		Headers: []table.Header{
			{Title: _titleEnvironment},
			{Title: _titleTaskAPI},
			{Title: _titleTaskJobCount},
			{Title: _titleLatestTaskJobID},
			{Title: _titleLastUpdated},
		},
		Rows: rows,
	}
}

func taskAPITable(taskAPI schema.APIResponse) string {
	jobRows := make([][]interface{}, 0, len(taskAPI.TaskJobStatuses))

	out := ""
	if len(taskAPI.TaskJobStatuses) == 0 {
		out = console.Bold("no submitted task jobs\n")
	} else {
		for _, job := range taskAPI.TaskJobStatuses {
			jobEndTime := time.Now()
			if job.EndTime != nil {
				jobEndTime = *job.EndTime
			}

			duration := jobEndTime.Sub(job.StartTime).Truncate(time.Second).String()

			jobRows = append(jobRows, []interface{}{
				job.ID,
				job.Status.Message(),
				job.StartTime.Format(_timeFormat),
				duration,
			})
		}

		t := table.Table{
			Headers: []table.Header{
				{Title: "task job id"},
				{Title: "status"},
				{Title: "start time"},
				{Title: "duration"},
			},
			Rows: jobRows,
		}

		out += t.MustFormat()
	}

	if taskAPI.DashboardURL != nil && *taskAPI.DashboardURL != "" {
		out += "\n" + console.Bold("metrics dashboard: ") + *taskAPI.DashboardURL + "\n"
	}

	if taskAPI.Endpoint != nil {
		out += "\n" + console.Bold("endpoint: ") + *taskAPI.Endpoint + "\n"
	}

	out += "\n" + apiHistoryTable(taskAPI.APIVersions)

	if !_flagVerbose {
		return out
	}

	out += titleStr("task api configuration") + taskAPI.Spec.UserStr()

	return out
}

func getTaskJob(env cliconfig.Environment, apiName string, jobID string) (string, error) {
	resp, err := cluster.GetTaskJob(MustGetOperatorConfig(env.Name), apiName, jobID)
	if err != nil {
		return "", err
	}

	var bytes []byte
	if _flagOutput == flags.JSONOutputType {
		bytes, err = libjson.Marshal(resp)
	} else if _flagOutput == flags.YAMLOutputType {
		bytes, err = yaml.Marshal(resp)
	}
	if err != nil {
		return "", err
	}
	if _flagOutput == flags.JSONOutputType || _flagOutput == flags.YAMLOutputType {
		return string(bytes), nil
	}

	job := resp.JobStatus

	out := ""

	jobIntroTable := table.KeyValuePairs{}
	jobIntroTable.Add("job id", job.ID)
	jobIntroTable.Add("status", job.Status.Message())
	out += jobIntroTable.String(&table.KeyValuePairOpts{BoldKeys: pointer.Bool(true)})

	jobTimingTable := table.KeyValuePairs{}
	jobTimingTable.Add("start time", job.StartTime.Format(_timeFormat))

	jobEndTime := time.Now()
	if job.EndTime != nil {
		jobTimingTable.Add("end time", job.EndTime.Format(_timeFormat))
		jobEndTime = *job.EndTime
	} else {
		jobTimingTable.Add("end time", "-")
	}
	duration := jobEndTime.Sub(job.StartTime).Truncate(time.Second).String()
	jobTimingTable.Add("duration", duration)

	out += "\n" + jobTimingTable.String(&table.KeyValuePairOpts{BoldKeys: pointer.Bool(true)})

	if job.Status.IsCompleted() {
		out += "\n" + "worker stats are not available because this job is not currently running\n"
	} else {
		out += titleStr("worker stats")
		if job.WorkerCounts != nil {
			t := table.Table{
				Headers: []table.Header{
					{Title: "Requested"},
					{Title: "Pending"},
					{Title: "Creating"},
					{Title: "Ready"},
					{Title: "NotReady"},
					{Title: "ErrImagePull", Hidden: job.WorkerCounts.ErrImagePull == 0},
					{Title: "Terminating", Hidden: job.WorkerCounts.Terminating == 0},
					{Title: "Failed", Hidden: job.WorkerCounts.Failed == 0},
					{Title: "Killed", Hidden: job.WorkerCounts.Killed == 0},
					{Title: "KilledOOM", Hidden: job.WorkerCounts.KilledOOM == 0},
					{Title: "Stalled", Hidden: job.WorkerCounts.Stalled == 0},
					{Title: "Unknown", Hidden: job.WorkerCounts.Unknown == 0},
					{Title: "Succeeded"},
				},
				Rows: [][]interface{}{
					{
						job.Workers,
						job.WorkerCounts.Pending,
						job.WorkerCounts.Creating,
						job.WorkerCounts.Ready,
						job.WorkerCounts.NotReady,
						job.WorkerCounts.ErrImagePull,
						job.WorkerCounts.Terminating,
						job.WorkerCounts.Failed,
						job.WorkerCounts.Killed,
						job.WorkerCounts.KilledOOM,
						job.WorkerCounts.Stalled,
						job.WorkerCounts.Unknown,
						job.WorkerCounts.Succeeded,
					},
				},
			}
			out += t.MustFormat(&table.Opts{BoldHeader: pointer.Bool(false)})
		} else {
			out += "unable to get worker stats\n"
		}
	}

	out += "\n" + console.Bold("job endpoint: ") + resp.Endpoint + "\n"

	jobSpecStr, err := libjson.Pretty(job.TaskJob)
	if err != nil {
		return "", err
	}

	out += titleStr("job configuration") + jobSpecStr

	return out, nil
}
