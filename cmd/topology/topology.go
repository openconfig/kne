package topology

import (
	"fmt"

	"github.com/google/kne/topo"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	topoCmd := &cobra.Command{
		Use:   "topology",
		Short: "Topology commands.",
	}
	topoCmd.AddCommand(watchCmd)
	return topoCmd
}

var (
	watchCmd = &cobra.Command{
		Use:   "watch <topology>",
		Short: "watch will watch the current topologies",
		RunE:  watchFn,
	}
)

func watchFn(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s: missing topology", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	s, err := cmd.Flags().GetString("kubecfg")
	if err != nil {
		return err
	}
	t, err := topo.New(s, topopb)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	if err := t.Watch(cmd.Context()); err != nil {
		return err
	}
	return nil
}
