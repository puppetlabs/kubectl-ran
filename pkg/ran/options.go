package ran

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	scheme "k8s.io/client-go/kubernetes/scheme"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const ContainerName = "worker"

type volumeSpec struct {
	src, dst string
}

type Options struct {
	ConfigFlags *genericclioptions.ConfigFlags
	EnvVars     []string
	Volumes     []string
	PodFile     string
	Cpu, Memory string
	WaitTimeout string
	Verbose     bool

	image   string
	command string
	args    []string

	genericclioptions.IOStreams
	namespace string
	config    *rest.Config
	client    kubernetes.Interface
	podInt    typedv1.PodInterface
	executor  executor

	pod         *corev1.Pod
	env         []corev1.EnvVar
	volumes     []volumeSpec
	cpu, memory resource.Quantity
	waitTimeout time.Duration
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

	o.podInt = o.client.CoreV1().Pods(o.namespace)
	o.executor = newExecutor(o.config, o.client.CoreV1().RESTClient())

	o.pod = &corev1.Pod{}
	if o.PodFile != "" {
		data, err := os.ReadFile(o.PodFile)
		if err != nil {
			return fmt.Errorf("failed reading pod file: %w", err)
		}

		newPod, _, err := scheme.Codecs.UniversalDeserializer().Decode(data, &schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, o.pod)
		if err != nil {
			return fmt.Errorf("pod file unmarshal: %w", err)
		}
		o.pod = newPod.(*corev1.Pod)
	}

	for _, envVar := range o.EnvVars {
		tuple := strings.Split(envVar, "=")
		if len(tuple) != 2 {
			return fmt.Errorf("%q was not formatted as name=value", envVar)
		}
		o.env = append(o.env, corev1.EnvVar{Name: tuple[0], Value: tuple[1]})
	}

	for _, volume := range o.Volumes {
		tuple := strings.Split(volume, ":")
		if len(tuple) != 2 {
			return fmt.Errorf("invalid volume spec %q, must be src:dst", volume)
		}
		o.volumes = append(o.volumes, volumeSpec{src: tuple[0], dst: tuple[1]})
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

	if o.waitTimeout, err = time.ParseDuration(o.WaitTimeout); err != nil {
		return fmt.Errorf("time %v is not a valid duration: %w", o.WaitTimeout, err)
	}

	return nil
}

func (o *Options) Run() error {
	ctx := context.TODO()

	if o.pod.ObjectMeta.Name == "" {
		o.pod.ObjectMeta.GenerateName = "kubectl-ran-"
	}
	var container *corev1.Container
	for i := range o.pod.Spec.Containers {
		if o.pod.Spec.Containers[i].Name == ContainerName {
			container = &o.pod.Spec.Containers[i]
		}
	}
	if container == nil {
		o.pod.Spec.Containers = append(o.pod.Spec.Containers, corev1.Container{Name: ContainerName})
		container = &o.pod.Spec.Containers[len(o.pod.Spec.Containers)-1]
	}

	container.Image = o.image
	if len(container.Command) == 0 {
		container.Command = []string{"tail"}
	}
	if len(container.Args) == 0 {
		container.Args = []string{"-f", "/dev/null"}
	}
	container.Env = append(container.Env, o.env...)

	if !o.cpu.IsZero() {
		container.Resources.Requests[corev1.ResourceCPU] = o.cpu
		container.Resources.Limits[corev1.ResourceCPU] = o.cpu
	}

	if !o.memory.IsZero() {
		container.Resources.Requests[corev1.ResourceMemory] = o.memory
		container.Resources.Limits[corev1.ResourceMemory] = o.memory
	}

	pod, err := o.podInt.Create(ctx, o.pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	defer func() {
		if err := o.podInt.Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0)); err != nil {
			o.Warn("failed to delete pod: %v", err)
		}
	}()

	return o.ExecInPod(ctx, pod)
}

func (o *Options) ExecInPod(ctx context.Context, pod *corev1.Pod) error {
	if err := o.waitForPodStart(ctx, pod.Name); err != nil {
		return err
	}

	for _, spec := range o.volumes {
		if err := o.copyToPod(ctx, spec.src, spec.dst, pod.Name); err != nil {
			return err
		}
	}

	execErr := o.executor.execute(pod.Name, o.namespace, append([]string{o.command}, o.args...),
		remotecommand.StreamOptions{Stdout: o.Out, Stderr: o.ErrOut})

	// preserve the exec error while also exposing any copy errors
	for _, spec := range o.volumes {
		if err := o.copyFromPod(ctx, spec.dst, spec.src, pod.Name); err != nil {
			if execErr != nil {
				o.Warn("failed to copy from pod:", err)
			} else {
				execErr = err
			}
		}
	}

	return execErr
}

var errPodTerminated = errors.New("pod terminated unexpectedly")

func (o *Options) waitForPodStart(ctx context.Context, name string) error {
	watcher, err := o.podInt.Watch(ctx, metav1.ListOptions{FieldSelector: "metadata.name=" + name})
	if err != nil {
		return err
	}
	defer watcher.Stop()

	ch := watcher.ResultChan()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return fmt.Errorf("channel error waiting for pod %q: %v", name, e)
			}
			switch e.Type {
			case watch.Modified:
				status := e.Object.(*corev1.Pod).Status
				switch status.Phase {
				case corev1.PodRunning:
					for _, condition := range status.Conditions {
						if condition.Type == corev1.PodReady {
							if condition.Status == corev1.ConditionTrue {
								// Success, we have a running pod.
								o.Info("pod %q ready", name)
								return nil
							}
							break
						}
					}
					o.Info("pod %q running: %v", name, summarizeConditions(status.Conditions))
				case corev1.PodSucceeded:
					return errPodTerminated
				case corev1.PodFailed:
					return errPodTerminated
				case corev1.PodUnknown:
					o.Warn("unknown state for pod %q: %v", name, e.Object)
				}
			case watch.Error:
				return fmt.Errorf("pod %q errored: %v", name, e.Object)
			}
		case <-time.After(o.waitTimeout):
			return fmt.Errorf("timed out waiting for pod %q", name)
		}
	}
}

func summarizeConditions(conditions []corev1.PodCondition) string {
	var b strings.Builder
	for _, condition := range conditions {
		fmt.Fprintf(&b, "%s=%s ", condition.Type, condition.Status)
	}
	return b.String()[:b.Len()-1]
}

func (o *Options) Warn(msg string, args ...interface{}) {
	if !strings.HasSuffix(msg, "\n") {
		msg = msg + "\n"
	}
	c := color.New(color.FgHiYellow)
	c.Printf(msg, args...)
}

func (o *Options) Info(msg string, args ...interface{}) {
	if o.Verbose {
		if !strings.HasSuffix(msg, "\n") {
			msg = msg + "\n"
		}
		c := color.New(color.FgHiCyan)
		c.Printf(msg, args...)
	}
}
