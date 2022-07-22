package canard

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
type TransferID uint8

/// High-level transport frame model.
type RxFrameModel struct {
	timestamp microsecond
	prority   Priority
	txKind    uint8
	port      PortID
	srcNode   NodeID
	dstNode   NodeID
	tid       TransferID
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

func tryParseFrame(ts microsecond, frame *Frame, out *RxFrameModel) error {
	if frame == nil || out == nil {
		return ErrInvalidArgument
	}
	if len(frame.payload) == 0 {
		return errEmptyPayload
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
	out.tid = TransferID(tail & TRANSFER_ID_MAX)
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
	if !valid {
		return errInvalidFrame
	}
	return nil
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

type InternalRxSession struct {
	txTimestamp      microsecond
	totalPayloadSize int
	payload          []byte
	crc              TxCRC
	tid              TransferID
	// Redundant Transport Index
	rti    uint8
	toggle bool
}

func (rxs *InternalRxSession) reset(txid TransferID, rti uint8) {
	rxs.totalPayloadSize = 0
	rxs.payload = rxs.payload[:0]
	rxs.crc = CRC_INITIAL
	rxs.tid = txid
	rxs.toggle = true // INITIAL TOGGLE STATE
	rxs.rti = rti
}

type TxMetadata struct {
}

type RxTransfer struct {
	metadata TxMetadata
	// The timestamp of the first received CAN frame of this transfer.
	// The time system may be arbitrary as long as the clock is monotonic (steady).
	timestamp microsecond
	payload   []byte
}

func (ins *Instance) RxAccept(timestamp microsecond, frame *Frame, rti uint8, outTx *RxTransfer, outSub *RxSub) error {
	if ins == nil || outTx == nil || frame == nil {
		return ErrInvalidArgument
	}
	if len(frame.payload) == 0 {
		return errEmptyPayload
	}
	model := RxFrameModel{}
	err := tryParseFrame(timestamp, frame, &model)
	if err != nil {
		return err
	}
	if NodeIDUnset != model.dstNode && ins.nodeID != model.dstNode {
		return ErrBadDstAddr
	}
	var sub *RxSub
	// GetRxSub
	// sub = cavlSearch()
	if outSub != nil {
		// set outsub to sub
	}
	if sub == nil {
		return ErrNoMatchingSub
	}

	return nil
}

func (ins *Instance) rxAcceptFrame(sub *RxSub, frame *RxFrameModel, rti uint8, outTx *RxTransfer) error {
	if sub == nil || frame == nil || outTx == nil {
		return ErrInvalidArgument
	}
	if len(frame.payload) == 0 {
		return errEmptyPayload
	}
	if frame.tid > TRANSFER_ID_MAX {
		return ErrBadTransferID
	}
	if NodeIDUnset != frame.dstNode && ins.nodeID != frame.dstNode {
		return ErrBadDstAddr
	}
	if frame.srcNode <= NODE_ID_MAX {
		// If such session does not exist, create it. This only makes sense if this is the first frame of a
		// transfer, otherwise, we won't be able to receive the transfer anyway so we don't bother.
		if sub.sessions[frame.srcNode] == nil && frame.txStart {
			sub.sessions[frame.srcNode] = &InternalRxSession{
				txTimestamp: frame.timestamp,
				crc:         CRC_INITIAL,
				tid:         frame.tid,
				rti:         rti,
				toggle:      true, // INITIAL_TOGGLE_STATE
			}
		}
		if sub.sessions[frame.srcNode] != nil {
			// return rxSessionUpdate()
		}
	} else {
		if frame.srcNode != NodeIDUnset {
			return ErrInvalidNodeID
		}
		// Anonymous transfer. Must allocate according to libcanard.
		payloadSize := min(sub.extent, len(frame.payload))
		payload := make([]byte, payloadSize)
		//rxInitTransferMetadataFromFrame(frame, &out_transfer->metadata);
		outTx.timestamp = frame.timestamp
		outTx.payload = payload
		copy(payload, frame.payload[:payloadSize])
	}
	return nil
}

func (ins *Instance) rxSessionUpdate(rxs *InternalRxSession, frame *RxFrameModel, rti uint8, txIdTimeout microsecond, extent int, outTx *RxTransfer) error {
	if rxs == nil || frame == nil || outTx == nil {
		return ErrInvalidArgument
	}
	if rxs.tid > TRANSFER_ID_MAX || frame.tid > TRANSFER_ID_MAX {
		return ErrBadTransferID
	}
	TIDTimeOut := frame.timestamp > rxs.txTimestamp && (frame.timestamp-rxs.txTimestamp) > txIdTimeout
	notPreviousTID := rxComputeTransferIDDifference(rxs.tid, frame.tid) > 1
	needRestart := TIDTimeOut || (rxs.rti == rti && frame.txStart && notPreviousTID)

	if needRestart {
		rxs.reset(frame.tid, rti)
	}
	if needRestart && !frame.txStart {
		// SOT miss.
		rxs.reset((rxs.tid+1)&TRANSFER_ID_MAX, rxs.rti) // RTI is retained
		rxs.payload = nil
		return errTODO // freed.
	}
	correctTransport := rxs.rti == rti
	correctToggle := frame.toggle == rxs.toggle
	correctTID := frame.tid == rxs.tid
	if correctTransport && correctToggle && correctTID {
		return ins.rxSessionAcceptFrame(rxs, frame, extent, outTx)
	}
	return errTODO
}

func rxComputeTransferIDDifference(a, b TransferID) uint8 {
	diff := int16(a) - int16(b)
	if diff < 0 {
		diff += 1 << TRANSFER_ID_BIT_LENGTH
	}
	return uint8(diff)
}

func (ins *Instance) rxSessionAcceptFrame(rxs *InternalRxSession, frame *RxFrameModel, extent int, outTx *RxTransfer) error {
	if rxs == nil || frame == nil || outTx == nil {
		return ErrInvalidArgument
	}
	if len(frame.payload) == 0 {
		return errEmptyPayload
	}
	if frame.tid > TRANSFER_ID_MAX {
		return ErrBadTransferID
	}

	if frame.txStart {
		rxs.txTimestamp = frame.timestamp
	}
	singleFrame := frame.txStart && frame.txEnd
	if !singleFrame {
		rxs.crc = rxs.crc.Add(frame.payload)
	}

	// out :=
	return nil
}

func (ins *Instance) rxSessionWritePayload(rxs *InternalRxSession, extent int, payload []byte) error {
	if rxs == nil {
		return ErrInvalidArgument
	}
	//  CANARD_ASSERT((payload != NULL) || (payload_size == 0U)); unreachable in go
	if len(rxs.payload) > extent || len(rxs.payload) > rxs.totalPayloadSize {
		return errTODO
	}
	rxs.totalPayloadSize += len(payload)
	if cap(rxs.payload) == 0 && extent > 0 {
		// Allocate the payload lazily, as late as possible.
		rxs.payload = make([]byte, extent)
	}
	bytesToCopy := len(payload)
	if len(rxs.payload)+bytesToCopy > extent {
		bytesToCopy = extent - len(rxs.payload)
	}

	// copy(rxs.payload, payload)
	// rxs.payload[:]
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

const (
	CRC_INITIAL    = 0xFFFF
	CRC_RESIDUE    = 0x0000
	CRC_SIZE_BYTES = 2
)

// Transfer priority level mnemonics per the recommendations given in the Cyphal Specification.
const (
	PriorityExceptional = iota
	PriorityImmediate
	PriorityFast
	PriorityHigh
	PriorityNominal // Nominal priority level should be the default.
	PriorityLow
	PrioritySlow
	PriorityOptional
)
