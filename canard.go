package gocanard

const _MTU = 64

type microsecond uint64

type TxItem struct {
	base          TxQueueItem
	payloadBuffer [_MTU]byte
}

type TxQueueItem struct {
	base     TreeNode
	nextInTx *TxQueueItem
	deadline microsecond
	frame    Frame
}

type Frame struct {
	extendedCANID uint32
	payload       []byte
}

type TreeNode struct {
	up *TreeNode
	lr [2]*TreeNode
	bf int8
}

type NodeID uint8

type Instance struct {
	userRef interface{}
	nodeID  NodeID
	// There are 3 kinds of transfer modes.
	rxSub [3]*TreeNode
}

type PortID uint32
type TransferID uint32

/// High-level transport frame model.
type RxFrameModel struct {
	timestamp microsecond
	prority   Priority
	txKind    uint8
	port      PortID
	srcNode   NodeID
	dstNode   NodeID
	txID      TransferID
	txStart   bool
	txEnd     bool
	toggle    bool
	payload   []byte
}

type Priority uint8

const (
	offset_Priority  = 26
	offset_SubjectID = 8
	offset_ServiceID = 14
	offset_DstNodeID = 7
)

func tryParseFrame(ts microsecond, frame *Frame, out *RxFrameModel) bool {
	if frame == nil || out == nil {
		panic("nil frame or model")
	}
	if len(frame.payload) == 0 {
		return false // no data.
	}
	valid := false
	canID := frame.extendedCANID
	out.prority = Priority(canID>>offset_Priority) & PRIORITY_MAX
	out.srcNode = NodeID(canID & NODE_ID_MAX)
	if 0 == canID&FLAG_SERVICE_NOT_MESSAGE {
		out.txKind = TxKindMessage
		out.port = PortID(canID>>offset_SubjectID) & SUBJECT_ID_MAX
		if canID&FLAG_ANONYMOUS_MESSAGE != 0 {
			out.srcNode = NodeIDUnset
		}
		out.dstNode = NodeIDUnset
		// Reserved bits may be unreserved in the future.
		valid = (0 == (canID & FLAG_RESERVED_23)) && (0 == (canID & FLAG_RESERVED_07))
	} else {
		if canID&FLAG_REQUEST_NOT_RESPONSE != 0 {
			out.txKind = TxKindRequest
		} else {
			out.txKind = TxKindResponse
		}
		out.port = PortID(canID>>offset_ServiceID) & SERVICE_ID_MAX
		out.dstNode = NodeID(canID>>offset_DstNodeID) & NODE_ID_MAX
		// The reserved bit may be unreserved in the future. It may be used to extend the service-ID to 10 bits.
		// Per Specification, source cannot be the same as the destination.
		valid = (0 == (canID & FLAG_RESERVED_23)) && (out.srcNode != out.dstNode)
	}

	// Payload parsing.
	tail := frame.payload[len(frame.payload)-1]
	out.payload = frame.payload[:len(frame.payload)-1] // Cut off the tail byte.

	// Tail byte parsing.
	// Intentional violation of MISRA: pointer arithmetics is required to locate the tail byte. Unavoidable.
	out.txID = TransferID(tail & TRANSFER_ID_MAX)
	out.txStart = (tail & TAIL_START_OF_TRANSFER) != 0
	out.txEnd = (tail & TAIL_END_OF_TRANSFER) != 0
	out.toggle = (tail & TAIL_TOGGLE) != 0

	// Final validation.
	// Protocol version check: if SOT is set, then the toggle shall also be set.
	// valid = valid && ((!out->start_of_transfer) || (INITIAL_TOGGLE_STATE == out->toggle));
	valid = valid && (!out.txStart || true == out.toggle) //
	// Anonymous transfers can be only single-frame transfers.
	valid = valid && ((out.txStart && out.txEnd) || (NodeIDUnset == out.srcNode))
	// Non-last frames of a multi-frame transfer shall utilize the MTU fully.
	valid = valid && ((len(out.payload) >= MFT_NON_LAST_FRAME_PAYLOAD_MIN) || out.txEnd)
	// A frame that is a part of a multi-frame transfer cannot be empty (tail byte not included).
	valid = valid && (len(out.payload) > 0 || (out.txStart && out.txEnd))
	return valid
}

/// Parameter ranges are inclusive; the lower bound is zero for all. See Cyphal/CAN Specification for background.
const (
	SUBJECT_ID_MAX         = 8191
	SERVICE_ID_MAX         = 511
	NODE_ID_MAX            = 127
	PRIORITY_MAX           = 7
	TRANSFER_ID_BIT_LENGTH = 5
	TRANSFER_ID_MAX        = ((1 << TRANSFER_ID_BIT_LENGTH) - 1)
)

const (
	FLAG_SERVICE_NOT_MESSAGE  = 1 << 25
	FLAG_ANONYMOUS_MESSAGE    = 1 << 24
	FLAG_REQUEST_NOT_RESPONSE = 1 << 24
	FLAG_RESERVED_23          = 1 << 23
	FLAG_RESERVED_07          = 1 << 7
)

const (
	TxKindMessage  = 0 ///< Multicast, from publisher to all subscribers.
	TxKindResponse = 1 ///< Point-to-point, from server to client.
	TxKindRequest  = 2 ///< Point-to-point, from client to server.
)

const (
	NodeIDUnset = 255
)

const (
	TAIL_START_OF_TRANSFER         = 128
	TAIL_END_OF_TRANSFER           = 64
	TAIL_TOGGLE                    = 32
	MFT_NON_LAST_FRAME_PAYLOAD_MIN = 7
)

type RxSub struct {
	base        TreeNode
	txIdTimeout microsecond
	extent      int
	port        PortID

	userRef interface{}

	sessions [NODE_ID_MAX]*InternalRxSession
}

type TxCRC uint8

type InternalRxSession struct {
	txTimestamp      microsecond
	totalPayloadSize int
	payload          []byte
	crc              TxCRC
	txID             TransferID
	redundantTxIdx   uint8
	toggle           bool
}
type TxMetadata struct {
	
}

type RxTransfer struct {
	metadata TxMetadata
	// The timestamp of the first received CAN frame of this transfer.
    // The time system may be arbitrary as long as the clock is monotonic (steady).
	timestamp microsecond
	payload []byte
}

func rxAcceptFrame(ins *Instance, sub *RxSub, frame *RxFrameModel,redundantTransportIdx uint8,outTx )
