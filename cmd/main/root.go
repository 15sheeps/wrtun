//go:build darwin || linux || windows

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/15sheeps/wrtun/pkg/tunnel/socks5"
)

var (
	logger *slog.Logger
	v      *viper.Viper
)

func init() {
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	slog.SetDefault(logger)

	v = viper.New()
	v.SetConfigFile(".env")
	v.SetConfigType("env")
	v.AddConfigPath(".")

	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(clientCmd)

	rootCmd.PersistentFlags().StringP(
		"transport", "T", string(MAXTransport),
		"Kind of transport that is used for signaling purpose",
	)
	rootCmd.PersistentFlags().StringP(
		"provider", "P", string(MAXProvider),
		"Kind of service that is used to retrieve ICE servers",
	)

	clientCmd.Flags().Uint16P("port", "p", 7331, "SOCKS5 client port")
}

var rootCmd = &cobra.Command{
	Use:   "wrtun",
	Short: "wrtun allows to tunnel traffic using WebRTC",
	Long: `A longer description that explains your CLI application in detail,
    including available commands and their usage.`,
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Act as a server",
	Long: `Server establishes peer connections with client over common
    transport and proxifies incoming connections via WebRTC datachannels.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		trStr, _ := cmd.Flags().GetString("transport")
		prStr, _ := cmd.Flags().GetString("provider")

		ctx := context.Background()

		tr, pr, err := configure(ctx, Transport(trStr), Provider(prStr))
		if err != nil {
			return err
		}

		tun := socks5.NewTunnel(tr, pr, logger)

		logger.Info("starting server",
			"transport", trStr,
			"provider", prStr,
		)

		if err := tun.StartServer(ctx); err != nil {
			logger.Error("server exited with error", "err", err)
			return err
		}

		return nil
	},
}

var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "Act as a client",
	Long: `Client establishes peer connections with server over common
    transport, listens SOCKS5 requests on specific port and sends them
    to server over datachannels.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		trStr, _ := cmd.Flags().GetString("transport")
		prStr, _ := cmd.Flags().GetString("provider")
		port, _ := cmd.Flags().GetUint16("port")

		ctx := context.Background()

		tr, pr, err := configure(ctx, Transport(trStr), Provider(prStr))
		if err != nil {
			return err
		}

		tun := socks5.NewTunnel(tr, pr, logger)

		addr := fmt.Sprintf("127.0.0.1:%d", port)

		logger.Info("starting client",
			"transport", trStr,
			"provider", prStr,
			"addr", addr,
		)

		if err := tun.StartClient(ctx, addr); err != nil {
			logger.Error("client exited with error", "err", err)
			return err
		}

		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
