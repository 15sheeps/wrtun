package main

import (
    "os"
    "time"
    "errors"
    "context"
    "log/slog"
    
    "github.com/spf13/cobra"
    "github.com/15sheeps/max-tunnel/transport/max"
    
)

func proceedMax(logger logger.LeveledLogger) *max.Client {

}

func proceedEmail() {
    
}

func proceedMax() {
    ctx := context.TODO()

    



    maxClient, err := max.NewClientWithContext(ctx, logger); 
    if err != nil {
        panic(err)
    }

    var fileLookupError viper.ConfigFileNotFoundError
    err = v.ReadInConfig()

    switch {
    case err == nil:
    case errors.As(err, &fileLookupError):
        logger.Warn("no .env file found")
    default:
        logger.Warn("error reading .env file: %w", err)
    }

    token := v.GetString("MAX_TOKEN")
    if token == "" {
        logger.Warn("MAX token is missing in .env file")
        
        if err := maxClient.Auth(ctx); err != nil {
            panic(err)
        }

        v.Set("MAX_TOKEN", maxClient.GetToken())
        if err := v.WriteConfig(); err != nil {
            logger.Warn("error writing token to .env file: %w", err)
        }
    } else {
        maxClient.Auth(ctx, token)
    }

    defer maxClient.Close()

    tun := socks5.NewTunnel(maxClient, maxClient, socks5.WithLogger(
        logger.With("where", "socks5 tunnel"),
    ))

    time.Sleep(1 * time.Hour)
}