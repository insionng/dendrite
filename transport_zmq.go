package dendrite

import (
	"github.com/golang/protobuf/proto"
	zmq "github.com/pebbe/zmq4"
	//"log"
	//"sync"
	"bytes"
	"fmt"
	"time"
)

const (
	// protocol buffer messages (for definitions, see pb_defs/chord.go)
	pbPing MsgType = iota
	pbAck
	pbErr
	pbForward
	pbJoin
	pbLeave
	pbListVnodes
	pbListVnodesResp
)

func (transport *ZMQTransport) newErrorMsg(msg string) *ChordMsg {
	pbmsg := &PBProtoErr{
		Error: proto.String(msg),
	}
	pbdata, _ := proto.Marshal(pbmsg)
	return &ChordMsg{
		Type: pbErr,
		Data: pbdata,
	}

}
func (transport *ZMQTransport) Encode(mt MsgType, data []byte) []byte {
	buf := new(bytes.Buffer)
	buf.WriteByte(byte(mt))
	buf.Write(data)
	return buf.Bytes()
}

func (transport *ZMQTransport) Decode(data []byte) (*ChordMsg, error) {
	data_len := len(data)
	if data_len == 0 {
		return nil, fmt.Errorf("data too short: %d", len(data))
	}

	cm := &ChordMsg{Type: MsgType(data[0])}

	if data_len > 1 {
		cm.Data = data[1:]
	}

	// parse the data and set the handler
	switch cm.Type {
	case pbPing:
		var pingMsg PBProtoPing
		err := proto.Unmarshal(cm.Data, &pingMsg)
		if err != nil {
			return nil, fmt.Errorf("error decoding PBProtoPing message - %s", err)
		}
		cm.TransportMsg = pingMsg
		cm.TransportHandler = transport.zmq_ping_handler
	case pbErr:
		var errorMsg PBProtoErr
		err := proto.Unmarshal(cm.Data, &errorMsg)
		if err != nil {
			return nil, fmt.Errorf("error decoding PBProtoErr message - %s", err)
		}
		cm.TransportMsg = errorMsg
		cm.TransportHandler = transport.zmq_error_handler
	case pbJoin:
		var joinMsg PBProtoJoin
		err := proto.Unmarshal(cm.Data, &joinMsg)
		if err != nil {
			return nil, fmt.Errorf("error decoding PBProtoJoin message - %s", err)
		}
		cm.TransportMsg = joinMsg
		cm.TransportHandler = transport.zmq_join_handler
	case pbLeave:
		var leaveMsg PBProtoLeave
		err := proto.Unmarshal(cm.Data, &leaveMsg)
		if err != nil {
			return nil, fmt.Errorf("error decoding PBProtoLeave message - %s", err)
		}
		cm.TransportMsg = leaveMsg
		cm.TransportHandler = transport.zmq_leave_handler
	case pbListVnodes:
		var listVnodesMsg PBProtoListVnodes
		err := proto.Unmarshal(cm.Data, &listVnodesMsg)
		if err != nil {
			return nil, fmt.Errorf("error decoding PBProtoListVnodes message - %s", err)
		}
		cm.TransportMsg = listVnodesMsg
		cm.TransportHandler = transport.zmq_listVnodes_handler
	case pbListVnodesResp:
		var listVnodesRespMsg PBProtoListVnodesResp
		err := proto.Unmarshal(cm.Data, &listVnodesRespMsg)
		if err != nil {
			return nil, fmt.Errorf("error decoding PBProtoListVnodesResp message - %s", err)
		}
		cm.TransportMsg = listVnodesRespMsg
	default:
		return nil, fmt.Errorf("error decoding message - unknown request type %x", cm.Type)
	}

	return cm, nil
}

// Client Request: list of vnodes from remote host
func (transport *ZMQTransport) ListVnodes(host string) ([]*Vnode, error) {
	req_sock, err := transport.zmq_context.NewSocket(zmq.REQ)
	if err != nil {
		return nil, err
	}
	err = req_sock.Connect("tcp://" + host)
	if err != nil {
		return nil, err
	}
	error_c := make(chan error, 1)
	resp_c := make(chan []*Vnode, 1)

	go func() {
		// Build request protobuf
		req := new(PBProtoListVnodes)
		reqData, _ := proto.Marshal(req)
		encoded := transport.Encode(pbListVnodes, reqData)
		req_sock.SendBytes(encoded, 0)

		// read response and decode it
		resp, err := req_sock.RecvBytes(0)
		if err != nil {
			error_c <- fmt.Errorf("Error while reading response - %s", err)
			return
		}
		decoded, err := transport.Decode(resp)
		if err != nil {
			error_c <- fmt.Errorf("error while decoding response for listVnodes - %s", err)
			return
		}

		switch decoded.Type {
		case pbErr:
			pbMsg := decoded.TransportMsg.(PBProtoErr)
			error_c <- fmt.Errorf("got error response - %s", pbMsg.GetError())
		case pbListVnodesResp:
			pbMsg := decoded.TransportMsg.(PBProtoListVnodesResp)
			vnodes := make([]*Vnode, len(pbMsg.GetVnodes()))
			for idx, pbVnode := range pbMsg.GetVnodes() {
				vnodes[idx] = &Vnode{Id: pbVnode.GetId(), Host: pbVnode.GetHost()}
			}
			resp_c <- vnodes
			return
		default:
			// unexpected response
			error_c <- fmt.Errorf("unexpected response for listVnodes")
			return
		}
	}()
	select {
	case <-time.After(transport.clientTimeout):
		return nil, fmt.Errorf("Command timed out!")
	case err := <-error_c:
		return nil, err
	case resp_vnodes := <-resp_c:
		return resp_vnodes, nil
	}

}

func (transport *ZMQTransport) Ping(remote_vn *Vnode) (bool, error) {
	req_sock, err := transport.zmq_context.NewSocket(zmq.REQ)
	if err != nil {
		return false, err
	}
	err = req_sock.Connect("tcp://" + remote_vn.Host)
	if err != nil {
		return false, err
	}
	pbPingMsg := &PBProtoPing{
		Version: proto.Int64(1),
	}
	pbPingData, _ := proto.Marshal(pbPingMsg)
	encoded := transport.Encode(pbPing, pbPingData)
	req_sock.SendBytes(encoded, 0)
	resp, _ := req_sock.RecvBytes(0)
	decoded, err := transport.Decode(resp)
	if err != nil {
		return false, err
	}
	pongMsg := new(PBProtoPing)
	err = proto.Unmarshal(decoded.Data, pongMsg)
	if err != nil {
		return false, err
	}
	fmt.Println("Got pong with version:", pongMsg.GetVersion())
	return true, nil
}