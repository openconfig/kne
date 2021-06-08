package topology

import (
	"fmt"
	"os"

	"github.com/google/kne/topo"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	topoCmd := &cobra.Command{
		Use:   "topology",
		Short: "Topology commands.",
	}
	topoCmd.AddCommand(pushCmd)
	topoCmd.AddCommand(watchCmd)
	return topoCmd
}

var (
	pushCmd = &cobra.Command{
		Use:   "push <topology> <device> <config file>",
		Short: "push config to device",
		RunE:  pushFn,
	}
	watchCmd = &cobra.Command{
		Use:   "watch <topology>",
		Short: "watch will watch the current topologies",
		RunE:  watchFn,
	}
)

func pushFn(cmd *cobra.Command, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("%s: missing args", cmd.Use)
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
	ctx := cmd.Context()
	t.Load(ctx)
	fp, err := os.Open(args[2])
	if err != nil {
		return err
	}
	if err := t.ConfigPush(ctx, args[1], fp); err != nil {
		return err
	}
	return nil
}

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
