package mydispatcher

import (
	"context"
	"time"

	B "github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/common/uot"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/net/cnc"
	"github.com/xtls/xray-core/common/signal"
	"github.com/xtls/xray-core/common/singbridge"
	"github.com/xtls/xray-core/transport"
	"github.com/xtls/xray-core/transport/pipe"
)

// uotMagic reports whether destination is one of the magic domains that mihomo and sing-box
// clients dial for UDP over TCP (`udp-over-tcp` on a Shadowsocks proxy), and which one.
func uotMagic(destination net.Destination) (string, bool) {
	if destination.Network != net.Network_TCP || !destination.Address.Family().IsDomain() {
		return "", false
	}
	switch domain := destination.Address.Domain(); domain {
	case uot.MagicAddress, uot.LegacyMagicAddress:
		return domain, true
	}
	return "", false
}

// dispatchUoT serves a UDP over TCP stream. xray-core's inbounds know nothing of it and would
// dial the magic domain as a TCP destination; sing-box servers unwrap it in their router, and so
// do we here. Each framed packet is dispatched as UDP under the connection's own session, so
// routing, the rate and device limits and traffic stats see an ordinary UDP flow from the client.
//
// For a relayed node this is how UDP keeps the client's address: the stream rides the relay's
// TCP leg, where the relay prepends a PROXY header, while plain UDP arrives from the relay's IP.
func (d *DefaultDispatcher) dispatchUoT(ctx context.Context, magic string) (*transport.Link, error) {
	opt := pipe.OptionsFromContext(ctx)
	uplinkReader, uplinkWriter := pipe.New(opt...)
	downlinkReader, downlinkWriter := pipe.New(opt...)
	conn := cnc.NewConnection(cnc.ConnectionInputMulti(downlinkWriter), cnc.ConnectionOutputMulti(uplinkReader))

	go func() {
		defer conn.Close()
		if err := serveUoT(ctx, conn, magic, d.Dispatch); err != nil {
			errors.LogInfoInner(ctx, err, "UDP over TCP stream ended")
		}
	}()

	return &transport.Link{Reader: downlinkReader, Writer: uplinkWriter}, nil
}

func serveUoT(ctx context.Context, conn net.Conn, magic string, dispatch func(context.Context, net.Destination) (*transport.Link, error)) error {
	// The legacy protocol (mihomo's default) has no request header: every packet carries its address.
	request := uot.Request{}
	if magic == uot.MagicAddress {
		r, err := uot.ReadRequest(conn)
		if err != nil {
			return errors.New("failed to read UDP over TCP request").Base(err)
		}
		request = *r
	}
	packetConn := uot.NewConn(conn, request)

	// Dispatched to the first packet's destination; later packets keep their own, as in
	// the Shadowsocks 2022 inbound's own UDP path.
	first := B.NewPacket()
	defer first.Release()
	firstAddr, err := packetConn.ReadPacket(first)
	if err != nil {
		return errors.New("failed to read the first UDP over TCP packet").Base(err)
	}
	destination, err := singbridge.ToDestination(firstAddr, net.Network_UDP)
	if err != nil {
		return err
	}
	errors.LogInfo(ctx, "tunnelling request to udp over tcp:", destination)

	link, err := dispatch(ctx, destination)
	if err != nil {
		return err
	}
	defer common.Close(link.Writer)
	defer common.Interrupt(link.Reader)

	outConn := &singbridge.PacketConnWrapper{
		Reader: link.Reader,
		Writer: link.Writer,
		Dest:   destination,
		T: signal.CancelAfterInactivity(ctx, func() {
			common.Interrupt(link.Reader)
		}, 300*time.Second),
	}
	if err := outConn.WritePacket(first, firstAddr); err != nil {
		return err
	}
	return bufio.CopyPacketConn(ctx, packetConn, outConn)
}
