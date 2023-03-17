package generic

import (
	"fmt"

	"go.uber.org/zap"
	batchV1 "k8s.io/api/batch/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/hellofresh/kangal/pkg/backends"
	loadTestV1 "github.com/hellofresh/kangal/pkg/kubernetes/apis/loadtest/v1"
)

var (
	loadTestLabelKey         = "app"
	loadTestMasterLabelValue = "loadtest-master"
	loadTestWorkerLabelValue = "loadtest-worker-pod"
	loadTestLabelName        = "name"
	primaryServicePort       = 9090
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
		BinaryData: map[string][]byte{
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

func newMasterJobName(loadTest loadTestV1.LoadTest) string {
	return fmt.Sprintf("%s-master", loadTest.ObjectMeta.Name)
}

func newMasterJob(
	loadTest loadTestV1.LoadTest,
	testfileConfigMap *coreV1.ConfigMap,
	envvarSecret *coreV1.Secret,
	reportURL string,
	masterResources backends.Resources,
	podAnnotations map[string]string,
	nodeSelector map[string]string,
	podTolerations []coreV1.Toleration,
	image loadTestV1.ImageDetails,
	logger *zap.Logger,
	config *Config,
) *batchV1.Job {
	name := newMasterJobName(loadTest)

	ownerRef := metaV1.NewControllerRef(&loadTest, loadTestV1.SchemeGroupVersion.WithKind("LoadTest"))

	imageRef := image
	if imageRef == "" {
		imageRef = loadTest.Spec.MasterConfig
		logger.Warn("Loadtest.Spec.MasterConfig is empty; using default image", zap.String("imageRef", string(imageRef)))
	}

	envVars := []coreV1.EnvVar{
		{Name: "GENERIC_TESTFILE", Value: "/data/testfile.json"},
		{Name: "NAME", Value: loadTest.ObjectMeta.Name},
		{Name: "PRIMARY_LISTEN_ADDR", Value: fmt.Sprintf(":%d", primaryServicePort)},
	}

	if reportURL != "" {
		envVars = append(envVars, coreV1.EnvVar{
			Name:  "REPORT_PRESIGNED_URL",
			Value: reportURL,
		})
	}

	if loadTest.Spec.Duration != 0 {
		envVars = append(envVars, coreV1.EnvVar{
			Name:  "DURATION",
			Value: loadTest.Spec.Duration.String(),
		})
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

	// Locust does not support recovering after a failure
	backoffLimit := int32(0)

	containerPorts := []coreV1.ContainerPort{}
	if config.MasterMetricsPortName != "" {
		containerPorts = append(containerPorts, coreV1.ContainerPort{
			Name:          config.MasterMetricsPortName,
			ContainerPort: config.MasterMetricsPort,
			Protocol:      coreV1.ProtocolTCP,
		})
	}

	return &batchV1.Job{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: loadTest.Status.Namespace,
			Labels: map[string]string{
				"name":           name,
				loadTestLabelKey: loadTestMasterLabelValue,
			},
			OwnerReferences: []metaV1.OwnerReference{*ownerRef},
		},
		Spec: batchV1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: coreV1.PodTemplateSpec{
				ObjectMeta: metaV1.ObjectMeta{
					Labels: map[string]string{
						"name":           name,
						loadTestLabelKey: loadTestMasterLabelValue,
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
							Resources: backends.BuildResourceRequirements(masterResources),
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

func newMasterService(loadTest loadTestV1.LoadTest, masterJob *batchV1.Job) *coreV1.Service {
	name := fmt.Sprintf("%s-master", loadTest.ObjectMeta.Name)

	ownerRef := metaV1.NewControllerRef(&loadTest, loadTestV1.SchemeGroupVersion.WithKind("LoadTest"))

	return &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: loadTest.Status.Namespace,
			Labels: map[string]string{
				loadTestLabelKey: name,
			},
			OwnerReferences: []metaV1.OwnerReference{*ownerRef},
		},
		Spec: coreV1.ServiceSpec{
			Selector:  masterJob.Spec.Template.Labels,
			ClusterIP: "None",
			Ports: []coreV1.ServicePort{
				{
					Name: "server",
					Port: int32(primaryServicePort),
					TargetPort: intstr.IntOrString{
						IntVal: int32(primaryServicePort),
					},
				},
			},
		},
	}
}

func newWorkerJobName(loadTest loadTestV1.LoadTest) string {
	return fmt.Sprintf("%s-worker", loadTest.ObjectMeta.Name)
}

func newWorkerJob(
	loadTest loadTestV1.LoadTest,
	testfileConfigMap *coreV1.ConfigMap,
	envvarSecret *coreV1.Secret,
	reportURL string,
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
		{Name: "PRIMARY_HOST", Value: fmt.Sprintf("%s:%d", masterService.GetName(), primaryServicePort)},
	}

	if reportURL != "" {
		envVars = append(envVars, coreV1.EnvVar{
			Name:  "REPORT_PRESIGNED_URL",
			Value: reportURL,
		})
	}

	if loadTest.Spec.Duration != 0 {
		envVars = append(envVars, coreV1.EnvVar{
			Name:  "DURATION",
			Value: loadTest.Spec.Duration.String(),
		})
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

	// master is also producing load so we need one less worker
	numWorkers := *loadTest.Spec.DistributedPods - 1

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
			Parallelism:  &numWorkers,
			Completions:  &numWorkers,
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
					TopologySpreadConstraints: []coreV1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "kubernetes.io/hostname",
							WhenUnsatisfiable: coreV1.ScheduleAnyway,
							LabelSelector: &metaV1.LabelSelector{
								MatchLabels: map[string]string{
									loadTestLabelName: loadTest.ObjectMeta.Name,
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
