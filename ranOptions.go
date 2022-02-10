package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type RanOptions struct {
	configFlags *genericclioptions.ConfigFlags
	image       string
	command     string
	args        []string
	envVars     []string

	genericclioptions.IOStreams
	namespace string
	config    *rest.Config
	env       []corev1.EnvVar
}

func NewRanOptions(streams genericclioptions.IOStreams) *RanOptions {
	return &RanOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		IOStreams:   streams,
	}
}

func (o *RanOptions) Validate(args []string) error {
	o.image = args[0]
	o.command = args[1]
	o.args = args[2:]

	var err error
	if o.namespace, _, err = o.configFlags.ToRawKubeConfigLoader().Namespace(); err != nil {
		return err
	}

	for _, envVar := range o.envVars {
		tuple := strings.Split(envVar, "=")
		if len(tuple) != 2 {
			return fmt.Errorf("'%v' was not formatted as name=value", envVar)
		}
		o.env = append(o.env, corev1.EnvVar{Name: tuple[0], Value: tuple[1]})
	}

	return nil
}

func (o *RanOptions) Run() error {
	ctx := context.TODO()

	var err error
	o.config, err = o.configFlags.ToRESTConfig()
	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfig(o.config)
	if err != nil {
		return err
	}
	podInterface := clientset.CoreV1().Pods(o.namespace)

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

	pod, err := podInterface.Create(ctx, podSpec, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	err = o.ExecInPod(ctx, clientset, podInterface, pod)

	// Always delete before returning the error.
	podInterface.Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
	return err
}

func (o *RanOptions) ExecInPod(ctx context.Context, clientset *kubernetes.Clientset, podInterface typedv1.PodInterface, pod *corev1.Pod) error {
	if err := o.waitForPodStart(ctx, podInterface, pod.Name); err != nil {
		return err
	}

	// TODO: copy volumes in

	execRequest := clientset.CoreV1().RESTClient().Post().
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

func (o *RanOptions) waitForPodStart(ctx context.Context, podInterface typedv1.PodInterface, name string) error {
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
