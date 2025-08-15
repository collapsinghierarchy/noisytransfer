package turn

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/pion/turn/v4"

	"github.com/collapsinghierarchy/noisytransfer/slogpion"
)

type Config struct {
	Realm, Username, Password string
	Logger                    *slog.Logger
}

func Start(ctx context.Context, cfg Config) error {
	udp, err := net.ListenPacket("udp4", ":3478")
	if err != nil {
		return fmt.Errorf("udp listen: %w", err)
	}
	tcpLn, err := net.Listen("tcp4", ":3478")
	if err != nil {
		return fmt.Errorf("tcp listen: %w", err)
	}

	// ----- auth --------------------------------------------------------------
	auth := func(user, realm string, _ net.Addr) ([]byte, bool) {
		if user != cfg.Username {
			return nil, false
		}
		return turn.GenerateAuthKey(user, realm, cfg.Password), true
	}

	// ----- Relay port-range --------------------------------------------------
	relay := &turn.RelayAddressGeneratorStatic{
		RelayAddress: net.ParseIP("127.0.0.1"),
		Address:      "0.0.0.0",
	}

	// ----- Server config (v4) ------------------------------------------------
	srvCfg := turn.ServerConfig{
		Realm:              cfg.Realm,
		AuthHandler:        auth,
		LoggerFactory:      slogpion.New(cfg.Logger.With("subsys", "pion.turn")),
		ChannelBindTimeout: 10 * time.Minute,
		InboundMTU:         1500,

		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn:            udp,
			RelayAddressGenerator: relay,
		}},
		ListenerConfigs: []turn.ListenerConfig{{
			Listener:              tcpLn,
			RelayAddressGenerator: relay,
		}},
	}

	srv, err := turn.NewServer(srvCfg)
	if err != nil {
		cfg.Logger.Error("turn start", "err", err)
		return err
	}
	cfg.Logger.Info("TURN ready",
		"public", ":3478")
	<-ctx.Done()
	srv.Close()
	return nil
}
