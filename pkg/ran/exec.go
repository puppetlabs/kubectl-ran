package ran

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type executor struct {
	config *rest.Config
	client rest.Interface
}

func newExecutor(config *rest.Config, client kubernetes.Interface) executor {
	var e executor
	e.config = config
	e.client = client.CoreV1().RESTClient()
	return e
}

func (e executor) execute(pod, namespace string, command []string, opts remotecommand.StreamOptions) error {
	req := e.client.Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec")

	if opts.Stdin != nil {
		req = req.Param("stdin", "true")
	}
	if opts.Stdout != nil {
		req = req.Param("stdout", "true")
	}
	if opts.Stderr != nil {
		req = req.Param("stderr", "true")
	}
	for _, cmd := range command {
		req = req.Param("command", cmd)
	}

	executor, err := remotecommand.NewSPDYExecutor(e.config, "POST", req.URL())
	if err != nil {
		return err
	}
	return executor.Stream(opts)
}
