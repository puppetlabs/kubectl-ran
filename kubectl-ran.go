// A utility for updating a Kubernetes TLS secret if it has expired or any of
// the inputs have changed.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	ranExample = `
	# Start busybox and run a command
	%[1]s ran busybox -- echo "Hello world"

	# Run a command with environment variables
	%[1]s ran busybox --env=VAR1=Hello --env=VAR2=world -- sh -c 'echo "$VAR1 $VAR2"'

	# Run a command with a synced directory
	%[1]s ran busybox --volume=./stuff:/stuff -- sh -c 'echo "Hello world" > /stuff/out.txt'
`
)

type RanOptions struct {
	configFlags *genericclioptions.ConfigFlags
	genericclioptions.IOStreams
	image   string
	envVars []string
}

func NewRanOptions(streams genericclioptions.IOStreams) *RanOptions {
	return &RanOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		IOStreams:   streams,
	}
}

func (o *RanOptions) Validate(args []string) error {
	o.image = args[0]
	return nil
}

func (o *RanOptions) Run() error {
	ctx := context.TODO()

	raw, err := o.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return err
	}
	namespace := raw.Contexts[raw.CurrentContext].Namespace

	config, err := o.configFlags.ToRESTConfig()
	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	podSpec := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			// TODO: generate unique name
			GenerateName: "ran",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "worker",
					Image: o.image,
					Args:  []string{"sleep", "604800"},
				},
			},
		},
	}

	pod, err := clientset.CoreV1().Pods(namespace).Create(ctx, podSpec, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	fmt.Println(pod.Name)
	clientset.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
	return nil
}

func NewCmdRan(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRanOptions(streams)

	cmd := &cobra.Command{
		Use:          `ran IMAGE [--env="key=value"] [--volume=src:dst] -- [COMMAND] [args...]`,
		Short:        "Run a command in an ephemeral container with synced volume and environment.",
		Example:      fmt.Sprintf(ranExample, "kubectl"),
		SilenceUsage: true,
		Args:         cobra.MinimumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Validate(args); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&o.envVars, "env", []string{}, "environment variables for the container")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func main() {
	flags := pflag.NewFlagSet("kubectl-ran", pflag.ExitOnError)
	pflag.CommandLine = flags

	root := NewCmdRan(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
