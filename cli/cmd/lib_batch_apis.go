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
	s "github.com/cortexlabs/cortex/pkg/lib/strings"
	"github.com/cortexlabs/cortex/pkg/lib/table"
	libtime "github.com/cortexlabs/cortex/pkg/lib/time"
	"github.com/cortexlabs/cortex/pkg/operator/schema"
	"github.com/cortexlabs/cortex/pkg/types/status"
)

const (
	_titleBatchAPI    = "batch api"
	_titleJobCount    = "running jobs"
	_titleLatestJobID = "latest job id"
)

func batchAPIsTable(batchAPIs []schema.APIResponse, envNames []string) table.Table {
	rows := make([][]interface{}, 0, len(batchAPIs))

	for i, batchAPI := range batchAPIs {
		if batchAPI.Metadata == nil {
			continue
		}
		lastAPIUpdated := time.Unix(batchAPI.Metadata.LastUpdated, 0)
		latestStartTime := time.Time{}
		latestJobID := "-"
		runningJobs := 0

		for _, job := range batchAPI.BatchJobStatuses {
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
			batchAPI.Metadata.Name,
			runningJobs,
			latestJobID,
			libtime.SinceStr(&lastAPIUpdated),
		})
	}

	return table.Table{
		Headers: []table.Header{
			{Title: _titleEnvironment},
			{Title: _titleBatchAPI},
			{Title: _titleJobCount},
			{Title: _titleLatestJobID},
			{Title: _titleLastUpdated},
		},
		Rows: rows,
	}
}

func batchAPITable(batchAPI schema.APIResponse) string {
	jobRows := make([][]interface{}, 0, len(batchAPI.BatchJobStatuses))

	out := ""
	if len(batchAPI.BatchJobStatuses) == 0 {
		out = console.Bold("no submitted batch jobs\n")
	} else {
		for _, job := range batchAPI.BatchJobStatuses {

			jobEndTime := time.Now()
			if job.EndTime != nil {
				jobEndTime = *job.EndTime
			}

			duration := jobEndTime.Sub(job.StartTime).Truncate(time.Second).String()

			jobRows = append(jobRows, []interface{}{
				job.ID,
				job.Status.Message(),
				job.TotalBatchCount,
				job.StartTime.Format(_timeFormat),
				duration,
			})
		}

		t := table.Table{
			Headers: []table.Header{
				{Title: "job id"},
				{Title: "status"},
				{Title: "total batches"},
				{Title: "start time"},
				{Title: "duration"},
			},
			Rows: jobRows,
		}

		out += t.MustFormat()
	}

	if batchAPI.DashboardURL != nil && *batchAPI.DashboardURL != "" {
		out += "\n" + console.Bold("metrics dashboard: ") + *batchAPI.DashboardURL + "\n"
	}

	if batchAPI.Endpoint != nil {
		out += "\n" + console.Bold("endpoint: ") + *batchAPI.Endpoint + "\n"
	}

	out += "\n" + apiHistoryTable(batchAPI.APIVersions)

	if !_flagVerbose {
		return out
	}

	out += titleStr("batch api configuration") + batchAPI.Spec.UserStr()

	return out
}

func getBatchJob(env cliconfig.Environment, apiName string, jobID string) (string, error) {
	resp, err := cluster.GetBatchJob(MustGetOperatorConfig(env.Name), apiName, jobID)
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

	succeeded := "-"
	failed := "-"
	avgTimePerBatch := "-"

	if resp.Metrics != nil {
		if resp.Metrics.AverageTimePerBatch != nil {
			batchMetricsDuration := time.Duration(*resp.Metrics.AverageTimePerBatch*1000000000) * time.Nanosecond
			avgTimePerBatch = batchMetricsDuration.Truncate(time.Millisecond).String()
		}

		succeeded = s.Int(resp.Metrics.Succeeded)
		failed = s.Int(resp.Metrics.Failed)
	}

	t := table.Table{
		Headers: []table.Header{
			{Title: "total"},
			{Title: "succeeded"},
			{Title: "failed attempts"},
			{Title: "avg time per batch"},
		},
		Rows: [][]interface{}{
			{
				job.TotalBatchCount,
				succeeded,
				failed,
				avgTimePerBatch,
			},
		},
	}

	out += titleStr("batch stats") + t.MustFormat(&table.Opts{BoldHeader: pointer.Bool(false)})

	if job.Status == status.JobEnqueuing {
		out += "\n" + "still enqueuing, workers have not been allocated for this job yet\n"
	} else if job.Status.IsCompleted() {
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

	jobSpecStr, err := libjson.Pretty(job.BatchJob)
	if err != nil {
		return "", err
	}

	out += titleStr("job configuration") + jobSpecStr

	return out, nil
}
