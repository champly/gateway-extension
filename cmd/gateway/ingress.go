package gateway

import (
	"github.com/champly/gateway-extension/pkg/controller"
	"github.com/champly/gateway-extension/pkg/kube"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

func NewIngressTransform() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "ingress",
		Short:        "ingress convert",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			PrintFlags(cmd.Flags())

			ctx := signals.SetupSignalHandler()

			// !import build manager plane client first
			if err := kube.InitManagerPlaneClusterClient(ctx); err != nil {
				return err
			}

			// build controller
			ctrl, err := controller.New(ctx)
			if err != nil {
				return err
			}
			return ctrl.Start()
		},
	}

	// manager-plane
	cmd.PersistentFlags().StringVarP(&kube.ManagerPlaneName, "manager_plane_name", "", kube.ManagerPlaneName, "manager plane client-go user-agent name")

	return cmd
}
