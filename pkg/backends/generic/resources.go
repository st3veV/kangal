package generic

import (
	"fmt"

	"go.uber.org/zap"
	batchV1 "k8s.io/api/batch/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hellofresh/kangal/pkg/backends"
	loadTestV1 "github.com/hellofresh/kangal/pkg/kubernetes/apis/loadtest/v1"
)

var (
	loadTestLabelKey         = "app"
	loadTestMasterLabelValue = "loadtest-master"
	loadTestWorkerLabelValue = "loadtest-worker-pod"
	loadTestLabelName        = "name"
)

func newConfigMapName(loadTest loadTestV1.LoadTest) string {
	return fmt.Sprintf("%s-testfile", loadTest.ObjectMeta.Name)
}

func newConfigMap(loadTest loadTestV1.LoadTest) *coreV1.ConfigMap {
	name := newConfigMapName(loadTest)

	ownerRef := metaV1.NewControllerRef(&loadTest, loadTestV1.SchemeGroupVersion.WithKind("LoadTest"))

	return &coreV1.ConfigMap{
		ObjectMeta: metaV1.ObjectMeta{
			Name:            name,
			Namespace:       loadTest.Status.Namespace,
			OwnerReferences: []metaV1.OwnerReference{*ownerRef},
		},
		Data: map[string]string{
			"testfile.json": loadTest.Spec.TestFile,
		},
	}
}

func newSecretName(loadTest loadTestV1.LoadTest) string {
	return fmt.Sprintf("%s-envvar", loadTest.ObjectMeta.Name)
}

func newSecret(loadTest loadTestV1.LoadTest, envs map[string]string) *coreV1.Secret {
	name := newSecretName(loadTest)

	ownerRef := metaV1.NewControllerRef(&loadTest, loadTestV1.SchemeGroupVersion.WithKind("LoadTest"))

	return &coreV1.Secret{
		ObjectMeta: metaV1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				loadTestLabelKey: name,
			},
			OwnerReferences: []metaV1.OwnerReference{*ownerRef},
		},
		StringData: envs,
	}
}

func newWorkerJobName(loadTest loadTestV1.LoadTest) string {
	return fmt.Sprintf("%s-worker", loadTest.ObjectMeta.Name)
}

func newWorkerJob(
	loadTest loadTestV1.LoadTest,
	testfileConfigMap *coreV1.ConfigMap,
	envvarSecret *coreV1.Secret,
	masterService *coreV1.Service,
	workerResources backends.Resources,
	podAnnotations map[string]string,
	nodeSelector map[string]string,
	podTolerations []coreV1.Toleration,
	image loadTestV1.ImageDetails,
	logger *zap.Logger,
	config *Config,
) *batchV1.Job {
	name := newWorkerJobName(loadTest)

	ownerRef := metaV1.NewControllerRef(&loadTest, loadTestV1.SchemeGroupVersion.WithKind("LoadTest"))

	envVars := []coreV1.EnvVar{
		{Name: "GENERIC_TESTFILE", Value: "/data/testfile.json"},
		{Name: "NAME", Value: loadTest.ObjectMeta.Name},
	}

	envFrom := make([]coreV1.EnvFromSource, 0)
	if envvarSecret != nil {
		envFrom = append(envFrom, coreV1.EnvFromSource{
			SecretRef: &coreV1.SecretEnvSource{
				LocalObjectReference: coreV1.LocalObjectReference{
					Name: newSecretName(loadTest),
				},
			},
		})
	}

	// No support for recovering after a failure
	backoffLimit := int32(0)

	containerPorts := []coreV1.ContainerPort{}
	if config.WorkerMetricsPortName != "" {
		containerPorts = append(containerPorts, coreV1.ContainerPort{
			Name:          config.WorkerMetricsPortName,
			ContainerPort: config.WorkerMetricsPort,
			Protocol:      coreV1.ProtocolTCP,
		})
	}

	return &batchV1.Job{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: loadTest.Status.Namespace,
			Labels: map[string]string{
				loadTestLabelKey:  name,
				loadTestLabelName: loadTest.ObjectMeta.Name,
			},
			OwnerReferences: []metaV1.OwnerReference{*ownerRef},
		},
		Spec: batchV1.JobSpec{
			Parallelism:  loadTest.Spec.DistributedPods,
			Completions:  loadTest.Spec.DistributedPods,
			BackoffLimit: &backoffLimit,
			Template: coreV1.PodTemplateSpec{
				ObjectMeta: metaV1.ObjectMeta{
					Labels: map[string]string{
						loadTestLabelKey:  loadTestWorkerLabelValue,
						loadTestLabelName: loadTest.ObjectMeta.Name,
					},
					Annotations: podAnnotations,
				},
				Spec: coreV1.PodSpec{
					NodeSelector:  nodeSelector,
					Tolerations:   podTolerations,
					RestartPolicy: "Never",
					Containers: []coreV1.Container{
						{
							Name:            "generic",
							Image:           string(image),
							ImagePullPolicy: "Always",
							Env:             envVars,
							VolumeMounts: []coreV1.VolumeMount{
								{
									Name:      "testfile",
									MountPath: "/data/testfile.json",
									SubPath:   "testfile.json",
								},
							},
							Resources: backends.BuildResourceRequirements(workerResources),
							EnvFrom:   envFrom,
							Ports:     containerPorts,
						},
					},
					Volumes: []coreV1.Volume{
						{
							Name: "testfile",
							VolumeSource: coreV1.VolumeSource{
								ConfigMap: &coreV1.ConfigMapVolumeSource{
									LocalObjectReference: coreV1.LocalObjectReference{
										Name: testfileConfigMap.GetName(),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// determineLoadTestPhaseFromJobs reads existing job statuses and determines what the loadtest status should be
func determineLoadTestPhaseFromJobs(jobs ...*batchV1.Job) loadTestV1.LoadTestPhase {
	for _, job := range jobs {
		if job.Status.Failed > int32(0) {
			return loadTestV1.LoadTestErrored
		}
		if job.Status.Active > int32(0) {
			return loadTestV1.LoadTestRunning
		}
		if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
			return loadTestV1.LoadTestStarting
		}
	}
	return loadTestV1.LoadTestFinished
}

// determineLoadTestStatusFromJobs reads existing job statuses and determines what the loadtest status should be
func determineLoadTestStatusFromJobs(jobs ...*batchV1.Job) batchV1.JobStatus {
	for _, job := range jobs {
		if job.Status.Failed > int32(0) {
			return job.Status
		}
	}
	for _, job := range jobs {
		if job.Status.Active > int32(0) {
			return job.Status
		}
	}

	return jobs[0].Status
}
