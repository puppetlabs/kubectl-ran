package ran

import (
	"net/url"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type executor struct {
	config *rest.Config
	client rest.Interface
	impl   func(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error)
}

func newExecutor(config *rest.Config, client rest.Interface) executor {
	var e executor
	e.config = config
	e.client = client
	e.impl = remotecommand.NewSPDYExecutor
	return e
}

func (e executor) execute(pod, namespace string, command []string, opts remotecommand.StreamOptions) error {
	req := e.client.Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec").
		Param("container", ContainerName)

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

	executor, err := e.impl(e.config, "POST", req.URL())
	if err != nil {
		return err
	}
	return executor.Stream(opts)
}
