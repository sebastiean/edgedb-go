package edgedb

// todo add context.Context

import (
	"fmt"
	"io"
	"net"

	"github.com/fmoor/edgedb-golang/edgedb/protocol"
	"github.com/fmoor/edgedb-golang/edgedb/protocol/aspect"
	"github.com/fmoor/edgedb-golang/edgedb/protocol/cardinality"
	"github.com/fmoor/edgedb-golang/edgedb/protocol/codecs"
	"github.com/fmoor/edgedb-golang/edgedb/protocol/format"
	"github.com/fmoor/edgedb-golang/edgedb/protocol/message"
)

// Conn client
type Conn struct {
	conn   io.ReadWriteCloser
	secret []byte
}

// ConnConfig options for configuring a connection
type ConnConfig struct {
	Database string
	User     string
	// todo support authentication etc.
}

// Close the db connection
func (edb *Conn) Close() error {
	// todo adjust return value if edb.conn.Close() close returns an error
	defer edb.conn.Close()
	msg := []byte{message.Terminate, 0, 0, 0, 4}
	if _, err := edb.conn.Write(msg); err != nil {
		return fmt.Errorf("error while terminating: %v", err)
	}
	return nil
}

func (edb *Conn) writeAndRead(bts []byte) []byte {
	// tmp := bts
	// for len(tmp) > 0 {
	// 	msg := protocol.PopMessage(&tmp)
	// 	fmt.Printf("writing message %q:\n% x\n", msg[0], msg)
	// }

	if _, err := edb.conn.Write(bts); err != nil {
		panic(err)
	}

	rcv := []byte{}
	n := 1024
	var err error
	for n == 1024 {
		tmp := make([]byte, 1024)
		n, err = edb.conn.Read(tmp)
		if err != nil {
			panic(err)
		}
		rcv = append(rcv, tmp[:n]...)
	}

	// tmp = rcv
	// for len(tmp) > 0 {
	// 	msg := protocol.PopMessage(&tmp)
	// 	fmt.Printf("read message %q:\n% x\n", msg[0], msg)
	// }

	return rcv
}

func (edb *Conn) Query(query string) (interface{}, error) {
	return edb.QueryWithArgs(query, map[string]interface{}{})
}

// Query the database
func (edb *Conn) QueryWithArgs(query string, args map[string]interface{}) (interface{}, error) {
	msg := []byte{message.Prepare, 0, 0, 0, 0}
	protocol.PushUint16(&msg, 0) // no headers
	protocol.PushUint8(&msg, format.Binary)
	protocol.PushUint8(&msg, cardinality.Many)
	protocol.PushBytes(&msg, []byte{}) // no statement name
	protocol.PushString(&msg, query)
	protocol.PutMsgLength(msg)

	pyld := msg
	pyld = append(pyld, message.Sync, 0, 0, 0, 4)
	var argumentCodecID protocol.UUID
	var resultCodecID protocol.UUID

	rcv := edb.writeAndRead(pyld)
	for len(rcv) > 4 {
		bts := protocol.PopMessage(&rcv)
		mType := protocol.PopUint8(&bts)

		switch mType {
		case message.PrepareComplete:
			protocol.PopUint32(&bts)                 // message length
			protocol.PopUint16(&bts)                 // number of headers, assume 0
			protocol.PopUint8(&bts)                  // cardianlity
			argumentCodecID = protocol.PopUUID(&bts) // argument type id
			resultCodecID = protocol.PopUUID(&bts)   // result type id
		case message.ReadyForCommand:
			break
		case message.ErrorResponse:
			protocol.PopUint32(&bts) // message length
			protocol.PopUint8(&bts)  // severity
			protocol.PopUint32(&bts) // code
			message := protocol.PopString(&bts)
			panic(message)
		}
	}

	msg = []byte{message.DescribeStatement, 0, 0, 0, 0}
	protocol.PushUint16(&msg, 0) // no headers
	protocol.PushUint8(&msg, aspect.DataDescription)
	protocol.PushUint32(&msg, 0) // no statement name
	protocol.PutMsgLength(msg)

	pyld = msg
	pyld = append(pyld, message.Sync, 0, 0, 0, 4)

	rcv = edb.writeAndRead(pyld)

	codecLookup := codecs.CodecLookup{}

	for len(rcv) > 4 {
		bts := protocol.PopMessage(&rcv)
		mType := protocol.PopUint8(&bts)

		switch mType {
		case message.CommandDataDescription:
			protocol.PopUint32(&bts)              // message length
			protocol.PopUint16(&bts)              // number of headers is always 0
			protocol.PopUint8(&bts)               // cardianlity
			protocol.PopUUID(&bts)                // argument descriptor ID
			descriptor := protocol.PopBytes(&bts) // argument descriptor
			for k, v := range codecs.Pop(&descriptor) {
				codecLookup[k] = v
			}
			protocol.PopUUID(&bts)               // result descriptor ID
			descriptor = protocol.PopBytes(&bts) // argument descriptor

			for k, v := range codecs.Pop(&descriptor) {
				codecLookup[k] = v
			}
		case message.ErrorResponse:
			protocol.PopUint32(&bts) // message length
			protocol.PopUint8(&bts)  // severity
			protocol.PopUint32(&bts) // code
			message := protocol.PopString(&bts)
			panic(message)
		}
	}

	argumentCodec := codecLookup[argumentCodecID]
	resultCodec := codecLookup[resultCodecID]

	msg = []byte{message.Execute, 0, 0, 0, 0}
	protocol.PushUint16(&msg, 0)       // no headers
	protocol.PushBytes(&msg, []byte{}) // no statement name
	argumentCodec.Encode(&msg, args)
	protocol.PutMsgLength(msg)

	pyld = msg
	pyld = append(pyld, message.Sync, 0, 0, 0, 4)

	rcv = edb.writeAndRead(pyld)
	out := []interface{}{}

	for len(rcv) > 0 {
		bts := protocol.PopMessage(&rcv)
		mType := protocol.PopUint8(&bts)

		switch mType {
		case message.Data:
			protocol.PopUint32(&bts) // message length
			protocol.PopUint16(&bts) // number of data elements (always 1)
			out = append(out, resultCodec.Decode(&bts))
		case message.CommandComplete:
			continue
		case message.ReadyForCommand:
			continue
		case message.ErrorResponse:
			protocol.PopUint32(&bts) // message length
			protocol.PopUint8(&bts)  // severity
			protocol.PopUint32(&bts) // code
			message := protocol.PopString(&bts)
			panic(message)
		}
	}
	return out, nil
}

// Connect to a database
func Connect(config ConnConfig) (edb *Conn, err error) {
	conn, err := net.Dial("tcp", "127.0.0.1:5656")
	if err != nil {
		return edb, fmt.Errorf("tcp connection error while connecting: %v", err)
	}
	edb = &Conn{conn, nil}

	msg := []byte{message.ClientHandshake, 0, 0, 0, 0}
	protocol.PushUint16(&msg, 0) // major version
	protocol.PushUint16(&msg, 8) // minor version
	protocol.PushUint16(&msg, 2) // number of parameters
	protocol.PushString(&msg, "database")
	protocol.PushString(&msg, config.Database)
	protocol.PushString(&msg, "user")
	protocol.PushString(&msg, config.User)
	protocol.PushUint16(&msg, 0) // no extensions
	protocol.PutMsgLength(msg)

	rcv := edb.writeAndRead(msg)

	var secret []byte

	for len(rcv) > 0 {
		bts := protocol.PopMessage(&rcv)

		switch protocol.PopUint8(&bts) {
		case message.ServerHandshake:
			// todo close the connection if protocol version can't be supported
			// https://edgedb.com/docs/internals/protocol/overview#connection-phase
		case message.ServerKeyData:
			secret = bts[5:]
		case message.ReadyForCommand:
			return &Conn{conn, secret}, nil
		case message.ErrorResponse:
			protocol.PopUint32(&bts) // message length
			protocol.PopUint8(&bts)  // severity
			protocol.PopUint32(&bts) // code
			message := protocol.PopString(&bts)
			panic(message)
		}
	}
	return edb, nil
}
