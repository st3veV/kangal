package generic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	batchV1 "k8s.io/api/batch/v1"

	loadTestV1 "github.com/hellofresh/kangal/pkg/kubernetes/apis/loadtest/v1"
)

func TestGetLoadTestStatusFromJobs(t *testing.T) {
	var scenarios = []struct {
		WorkerJob *batchV1.Job
		Expected  loadTestV1.LoadTestPhase
	}{
		{ // Starting
			WorkerJob: &batchV1.Job{},
			Expected:  loadTestV1.LoadTestStarting,
		},
		{ // two workers, all running

			WorkerJob: &batchV1.Job{
				Status: batchV1.JobStatus{
					Active: int32(2),
				},
			},
			Expected: loadTestV1.LoadTestRunning,
		},
		{ // One worker failed
			WorkerJob: &batchV1.Job{
				Status: batchV1.JobStatus{
					Active: int32(1),
					Failed: int32(1),
				},
			},
			Expected: loadTestV1.LoadTestErrored,
		},
		{ // Both succeeded
			WorkerJob: &batchV1.Job{
				Status: batchV1.JobStatus{
					Succeeded: int32(2),
				},
			},
			Expected: loadTestV1.LoadTestFinished,
		},
	}

	for _, scenario := range scenarios {
		actual := determineLoadTestPhaseFromJobs(scenario.WorkerJob)
		assert.Equal(t, scenario.Expected, actual)
	}
}
