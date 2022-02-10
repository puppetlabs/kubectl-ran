package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"k8s.io/cli-runtime/pkg/genericclioptions"
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
