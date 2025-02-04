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

package gateway

import (
	"time"

	"github.com/cortexlabs/cortex/pkg/types/async"
)

// UserResponse represents the user's API response, which has to be JSON serializable
type UserResponse = map[string]interface{}

// CreateWorkloadResponse represents the response returned to the user on workload creation
type CreateWorkloadResponse struct {
	ID string `json:"id"`
}

// GetWorkloadResponse represents the workload response that is returned to the user
type GetWorkloadResponse struct {
	ID        string        `json:"id"`
	Status    async.Status  `json:"status"`
	Result    *UserResponse `json:"result,omitempty"`
	Timestamp *time.Time    `json:"timestamp,omitempty"`
}
