package generic

// Config specific to Generic backend
type Config struct {
	Image string `envconfig:"GENERIC_IMAGE"`

	MasterCPULimits       string `envconfig:"GENERIC_MASTER_CPU_LIMITS"`
	MasterCPURequests     string `envconfig:"GENERIC_MASTER_CPU_REQUESTS"`
	MasterMemoryLimits    string `envconfig:"GENERIC_MASTER_MEMORY_LIMITS"`
	MasterMemoryRequests  string `envconfig:"GENERIC_MASTER_MEMORY_REQUESTS"`
	MasterMetricsPort     int32  `envconfig:"GENERIC_MASTER_METRICS_PORT"`
	MasterMetricsPortName string `envconfig:"GENERIC_MASTER_METRICS_PORT_NAME"`

	WorkerCPULimits       string `envconfig:"GENERIC_WORKER_CPU_LIMITS"`
	WorkerCPURequests     string `envconfig:"GENERIC_WORKER_CPU_REQUESTS"`
	WorkerMemoryLimits    string `envconfig:"GENERIC_WORKER_MEMORY_LIMITS"`
	WorkerMemoryRequests  string `envconfig:"GENERIC_WORKER_MEMORY_REQUESTS"`
	WorkerMetricsPort     int32  `envconfig:"GENERIC_WORKER_METRICS_PORT"`
	WorkerMetricsPortName string `envconfig:"GENERIC_WORKER_METRICS_PORT_NAME"`
}
