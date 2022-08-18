package canard

import (
	"errors"
)

type Instance struct {
	// userRef any
	NodeID NodeID
	// There are 3 kinds of transfer modes.
	rxSub [3]*TreeNode
}

const _MTU = 64

type microsecond uint64

type TxItem struct {
	base          TxQueueItem
	payloadBuffer [_MTU]byte
}

// Tail is the last byte of the payload and contains transfer
// control flow data such as if the transfer is a start/end frame
// and if toggle bit is set.
type Tail byte

func (t Tail) IsToggled() bool { return t&TAIL_TOGGLE != 0 }
func (t Tail) IsStart() bool   { return t&TAIL_START_OF_TRANSFER != 0 }
func (t Tail) IsEnd() bool     { return t&TAIL_END_OF_TRANSFER != 0 }
func (t Tail) TransferID() TID { return TID(t & TRANSFER_ID_MAX) }

type TxQueue struct {
	// The maximum number of frames this queue is allowed to contain. An attempt to push more will fail with an
	// out-of-memory error even if the memory is not exhausted. This value can be changed by the user at any moment.
	// The purpose of this limitation is to ensure that a blocked queue does not exhaust the heap memory.
	Cap int
	// The transport-layer maximum transmission unit (MTU). The value can be changed arbitrarily at any time between
	// pushes. It defines the maximum number of data bytes per CAN data frame in outgoing transfers via this queue.
	//
	// Only the standard values should be used as recommended by the specification;
	// otherwise, networking interoperability issues may arise. See recommended values CANARD_MTU_*.
	//
	// Valid values are any valid CAN frame data length value not smaller than 8.
	// Invalid values are treated as the nearest valid value. The default is the maximum valid value.
	MTU  int
	size int
	root *TreeNode
	// userRef any
}
type TxQueueItem struct {
	// Must be first field due to use of unsafe.
	base     TreeNode
	nextInTx *TxQueueItem
	deadline microsecond
	frame    Frame
}

func (t *TxQueueItem) TailByte() Tail {
	if t.frame.payloadSize < 1 {
		panic("empty payload")
	}
	return Tail(t.frame.payload[t.frame.payloadSize-1])
}

func (t *TxQueueItem) CRC() (CRC, error) {
	if t.frame.payloadSize < 4 {
		return 0, errors.New("payload too small for CRC")
	}
	if !t.TailByte().IsEnd() {
		return 0, errors.New("CRC only set on End transfers")
	}
	return CRC(t.frame.payload[t.frame.payloadSize-3])<<8 |
		CRC(t.frame.payload[t.frame.payloadSize-2]), nil
}

type Frame struct {
	extendedCANID uint32
	payloadSize   int
	payload       []byte
}

type NodeID uint8

//go:inline
func (n NodeID) IsValid() bool {
	return n.IsSet() || n.IsUnset()
}

//go:inline
func (n NodeID) IsUnset() bool { return n == 0xff }

//go:inline
func (n NodeID) IsSet() bool { return n <= NODE_ID_MAX }

//go:inline
func (n *NodeID) Unset() { *n = 0xff }

type PortID uint32

// Transfer ID
type TID uint8

// / High-level transport frame model.
type FrameModel struct {
	timestamp   microsecond
	prority     Priority
	txKind      TxKind
	port        PortID
	srcNode     NodeID
	dstNode     NodeID
	tid         TID
	txStart     bool
	txEnd       bool
	toggle      bool
	payloadSize int
	payload     []byte
}

func (tx *Metadata) fromRxFrame(frame *FrameModel) {
	tx.Priority = frame.prority
	tx.TxKind = frame.txKind
	tx.Port = frame.port
	tx.Remote = frame.srcNode
	tx.TID = frame.tid
}

const nodemax = 127

type Sub struct {
	// must be first field due to use of unsafe.
	base       TreeNode
	tidTimeout microsecond
	extent     int
	port       PortID
	userRef    interface{}
	sessions   [nodemax]*internalRxSession
}

type Metadata struct {
	Priority Priority
	TxKind   TxKind
	Port     PortID
	Remote   NodeID
	TID      TID
}

type Transfer struct {
	metadata Metadata
	// The timestamp of the first received CAN frame of this transfer.
	// The time system may be arbitrary as long as the clock is monotonic (steady).
	timestamp   microsecond
	payloadSize int
	payload     []byte
}

// ecID represents an extended CAN ID.
type ecID uint32

func (can ecID) Priority() Priority  { return Priority(can>>offset_Priority) & priorityMask }
func (can ecID) Source() NodeID      { return NodeID(can & NODE_ID_MAX) }
func (can ecID) Destination() NodeID { return NodeID((can >> offset_DstNodeID) & NODE_ID_MAX) }
func (can ecID) IsMessage() bool     { return can&FLAG_SERVICE_NOT_MESSAGE == 0 }
func (can ecID) IsRequest() bool {
	return !can.IsMessage() && can&FLAG_REQUEST_NOT_RESPONSE != 0
}
func (can ecID) IsAnonymous() bool { return can&FLAG_ANONYMOUS_MESSAGE != 0 }
func (can ecID) PortID() PortID {
	if can.IsMessage() {
		return PortID(can>>offset_SubjectID) & SUBJECT_ID_MAX
	}
	return PortID(can>>offset_ServiceID) & SERVICE_ID_MAX
}

func (m *Metadata) makeCANID(payloadSize int, payload []byte, local NodeID, presentationLayerMTU int) (uint32, error) {
	switch {
	case presentationLayerMTU <= 0:
		return 0, ErrInvalidArgument
	case len(payload) == 0 && payloadSize != 0:
		return 0, errEmptyPayload
	case len(payload) < payloadSize:
		panic("OOM payload TODO")
	}
	var out uint32
	if m.TxKind == TxKindMessage && m.Remote.IsUnset() && m.Port <= SUBJECT_ID_MAX {
		if local <= NODE_ID_MAX {
			out = makeMessageSessionSpecifier(m.Port, local)
		} else if payloadSize <= presentationLayerMTU {
			c := newNodeID(payload[:payloadSize])
			out = makeMessageSessionSpecifier(m.Port, c) | FLAG_ANONYMOUS_MESSAGE
			if out > _CAN_EXT_ID_MASK {
				panic("spec > can_ext_id_mask TODO")
			}
		} else {
			return 0, ErrInvalidArgument
		}
	} else if (m.TxKind == TxKindRequest || m.TxKind == TxKindResponse) && m.Remote.IsSet() && m.Port <= SERVICE_ID_MAX {
		if !local.IsSet() {
			return 0, ErrInvalidArgument
		}
		out = makeServiceSessionSpecifier(m.Port, m.TxKind, local, m.Remote)
	}
	if out == 0 {
		panic("Invalid arg or undefined behaviour")
	}
	prio := m.Priority
	if prio >= numOfPriorities {
		return 0, ErrInvalidArgument
	}
	out |= uint32(prio) << offset_Priority
	return out, nil
}

func newNodeID(data []byte) NodeID {
	return NodeID((CRC).Add(newCRC(), data)) & NODE_ID_MAX
}

func makeMessageSessionSpecifier(subject PortID, src NodeID) uint32 {
	if src > NODE_ID_MAX || subject > SUBJECT_ID_MAX {
		panic("bad src or subject")
	}
	aux := subject | (SUBJECT_ID_MAX + 1) | ((SUBJECT_ID_MAX + 1) * 2)
	return uint32(src) | uint32(aux<<offset_SubjectID)
}

func makeServiceSessionSpecifier(service PortID, kind TxKind, src, dst NodeID) (spec uint32) {
	switch {
	case kind != TxKindResponse && kind != TxKindRequest:
		panic("kind must be response or request")
	case !src.IsSet() || !dst.IsSet():
		panic("src and dst must be set")
	case service > SERVICE_ID_MAX:
		panic("serviceID > max")
	}
	spec = uint32(src) | uint32(dst)<<offset_DstNodeID
	spec |= uint32(service << offset_ServiceID)
	spec |= uint32(b2i(kind == TxKindRequest)) << 24
	spec |= FLAG_SERVICE_NOT_MESSAGE
	return spec
}
