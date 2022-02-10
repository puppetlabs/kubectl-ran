package ran

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

type Options struct {
	ConfigFlags *genericclioptions.ConfigFlags
	EnvVars     []string
	Cpu, Memory string

	image   string
	command string
	args    []string

	genericclioptions.IOStreams
	namespace   string
	config      *rest.Config
	client      kubernetes.Interface
	env         []corev1.EnvVar
	cpu, memory resource.Quantity
}

func NewOptions(streams genericclioptions.IOStreams) *Options {
	return &Options{
		ConfigFlags: genericclioptions.NewConfigFlags(true),
		IOStreams:   streams,
	}
}

func (o *Options) Validate(args []string) error {
	o.image = args[0]
	o.command = args[1]
	o.args = args[2:]

	var err error
	if o.namespace, _, err = o.ConfigFlags.ToRawKubeConfigLoader().Namespace(); err != nil {
		return err
	}

	o.config, err = o.ConfigFlags.ToRESTConfig()
	if err != nil {
		return err
	}

	o.client, err = kubernetes.NewForConfig(o.config)
	if err != nil {
		return err
	}

	for _, envVar := range o.EnvVars {
		tuple := strings.Split(envVar, "=")
		if len(tuple) != 2 {
			return fmt.Errorf("'%v' was not formatted as name=value", envVar)
		}
		o.env = append(o.env, corev1.EnvVar{Name: tuple[0], Value: tuple[1]})
	}

	if o.Cpu != "" {
		o.cpu, err = resource.ParseQuantity(o.Cpu)
		if err != nil {
			return fmt.Errorf("cpu: %w", err)
		}
	}

	if o.Memory != "" {
		o.memory, err = resource.ParseQuantity(o.Memory)
		if err != nil {
			return fmt.Errorf("memory: %w", err)
		}
	}

	return nil
}

func (o *Options) Run() error {
	ctx := context.TODO()

	podInterface := o.client.CoreV1().Pods(o.namespace)

	podSpec := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "kubectl-ran-",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "worker",
					Image:   o.image,
					Command: []string{"tail"},
					Args:    []string{"-f", "/dev/null"},
					Env:     o.env,
				},
			},
		},
	}

	if !o.cpu.IsZero() {
		podSpec.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = o.cpu
		podSpec.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = o.cpu
	}

	if !o.memory.IsZero() {
		podSpec.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = o.memory
		podSpec.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = o.memory
	}

	pod, err := podInterface.Create(ctx, podSpec, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	defer func() {
		if err := podInterface.Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0)); err != nil {
			fmt.Println("failed to delete pod:", err)
		}
	}()

	return o.ExecInPod(ctx, podInterface, pod)
}

func (o *Options) ExecInPod(ctx context.Context, podInterface typedv1.PodInterface, pod *corev1.Pod) error {
	if err := o.waitForPodStart(ctx, podInterface, pod.Name); err != nil {
		return err
	}

	// TODO: copy volumes in

	execRequest := o.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(o.namespace).
		SubResource("exec").
		Param("stdout", "true").
		Param("stderr", "true").
		Param("command", o.command)
	for _, arg := range o.args {
		execRequest = execRequest.Param("command", arg)
	}
	executor, err := remotecommand.NewSPDYExecutor(o.config, "POST", execRequest.URL())
	if err != nil {
		return err
	}
	err = executor.Stream(remotecommand.StreamOptions{Stdout: o.Out, Stderr: o.ErrOut})

	// TODO: copy volumes out, deal with error handling; we want to preserve the exec error while also exposing any copy errors

	return err
}

var errPodTerminated = errors.New("pod terminated unexpectedly")

func (o *Options) waitForPodStart(ctx context.Context, podInterface typedv1.PodInterface, name string) error {
	watcher, err := podInterface.Watch(ctx, metav1.ListOptions{FieldSelector: "metadata.name=" + name})
	if err != nil {
		return err
	}
	defer watcher.Stop()

	ch := watcher.ResultChan()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return fmt.Errorf("channel error waiting for pod %v: %v", name, e)
			}
			switch e.Type {
			case watch.Modified:
				switch e.Object.(*corev1.Pod).Status.Phase {
				case corev1.PodRunning:
					// Success, we have a running pod.
					return nil
				case corev1.PodSucceeded:
					return errPodTerminated
				case corev1.PodFailed:
					return errPodTerminated
				case corev1.PodUnknown:
					fmt.Printf("unknown state for pod %v: %v\n", name, e.Object)
				}
			case watch.Error:
				return fmt.Errorf("pod %v errored: %v", name, e.Object)
			}
		case <-time.After(30 * time.Second):
			return fmt.Errorf("timed out waiting for pod %v", name)
		}
	}
}
