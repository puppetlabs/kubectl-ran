package main

import (
	"fmt"
	"os"

	"github.com/puppetlabs/kubectl-ran/pkg/ran"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"k8s.io/cli-runtime/pkg/genericclioptions"
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

	version = "dev"
)

func NewCmdRan(streams genericclioptions.IOStreams) *cobra.Command {
	o := ran.NewOptions(streams)

	cmd := &cobra.Command{
		Use:          `kubectl-ran IMAGE [--env="key=value"] [--volume=src:dst] -- COMMAND [args...]`,
		Short:        "Run a command in an ephemeral container with synced volume and environment.",
		Example:      fmt.Sprintf(ranExample, "kubectl"),
		SilenceUsage: true,
		Version:      version,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return c.Help()
			}
			if err := cobra.MinimumNArgs(2)(c, args); err != nil {
				return err
			}

			if err := o.Validate(args); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&o.PodFile, "pod-file", "f", "", "YAML file containing a pod definition; container named '"+ran.ContainerName+"' will be overwritten with specified image and used for command execution")
	cmd.Flags().StringArrayVarP(&o.EnvVars, "env", "e", []string{}, "environment variables for the container")
	cmd.Flags().StringArrayVarP(&o.Volumes, "volume", "v", []string{}, "volumes to sync")
	cmd.Flags().StringVar(&o.Cpu, "cpu", "", "cpu requirements for the container")
	cmd.Flags().StringVar(&o.Cpu, "memory", "", "memory requirements for the container")
	cmd.Flags().StringVar(&o.WaitTimeout, "wait-timeout", "30s", "time to wait for the pod to be ready")
	cmd.Flags().BoolVar(&o.Verbose, "verbose", false, "display verbose output")
	o.ConfigFlags.AddFlags(cmd.Flags())

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
