package canard

type Instance struct {
	userRef interface{}
	nodeID  NodeID
	// There are 3 kinds of transfer modes.
	rxSub [3]*TreeNode
}

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
	payloadSize   int
	payload       []byte
}

type NodeID uint8

type PortID uint32
type TransferID uint8

/// High-level transport frame model.
type RxFrameModel struct {
	timestamp   microsecond
	prority     Priority
	txKind      uint8
	port        PortID
	srcNode     NodeID
	dstNode     NodeID
	tid         TransferID
	txStart     bool
	txEnd       bool
	toggle      bool
	payloadSize int
	payload     []byte
}

type RxSub struct {
	base       TreeNode
	tidTimeout microsecond
	extent     int
	port       PortID

	userRef interface{}

	sessions [NODE_ID_MAX]*InternalRxSession
}

type InternalRxSession struct {
	txTimestamp      microsecond
	totalPayloadSize int
	payloadSize      int
	payload          []byte
	crc              TxCRC
	tid              TransferID
	// Redundant Transport Index
	rti    uint8
	toggle bool
}

type TxMetadata struct {
}

type RxTransfer struct {
	metadata TxMetadata
	// The timestamp of the first received CAN frame of this transfer.
	// The time system may be arbitrary as long as the clock is monotonic (steady).
	timestamp   microsecond
	payloadSize int
	payload     []byte
}

type Priority uint8

func rxTryParseFrame(ts microsecond, frame *Frame, out *RxFrameModel) error {
	switch {
	case frame == nil || out == nil:
		return ErrInvalidArgument
	case frame.payloadSize > 0:
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
	out.payloadSize = frame.payloadSize - 1
	out.payload = frame.payload // Cut off the tail byte.

	// Tail byte parsing.
	// No violation of MISRA.
	tail := frame.payload[out.payloadSize-1]
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
	valid = valid && ((out.payloadSize >= MFT_NON_LAST_FRAME_PAYLOAD_MIN) || out.txEnd)
	// A frame that is a part of a multi-frame transfer cannot be empty (tail byte not included).
	valid = valid && (out.payloadSize > 0 || (out.txStart && out.txEnd))
	if !valid {
		return errInvalidFrame
	}
	return nil
}

func (rxs *InternalRxSession) reset(txid TransferID, rti uint8) {
	rxs.totalPayloadSize = 0
	rxs.payload = rxs.payload[:0]
	rxs.crc = CRC_INITIAL
	rxs.tid = txid
	rxs.toggle = true // INITIAL TOGGLE STATE
	rxs.rti = rti
}

func (ins *Instance) RxAccept(timestamp microsecond, frame *Frame, rti uint8, outTx *RxTransfer, outSub *RxSub) error {
	switch {
	case ins == nil || outTx == nil || frame == nil:
		return ErrInvalidArgument
	case len(frame.payload) == 0:
		return errEmptyPayload
	}

	model := RxFrameModel{}
	err := rxTryParseFrame(timestamp, frame, &model)
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
	switch {
	case sub == nil || frame == nil || outTx == nil:
		return ErrInvalidArgument
	case len(frame.payload) == 0:
		return errEmptyPayload
	case frame.tid > TRANSFER_ID_MAX:
		return ErrBadTransferID
	case NodeIDUnset != frame.dstNode && ins.nodeID != frame.dstNode:
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
			return ins.rxSessionUpdate(sub.sessions[frame.srcNode], frame,
				rti, sub.tidTimeout, sub.extent, outTx)
		}
	} else {
		if frame.srcNode != NodeIDUnset {
			return ErrInvalidNodeID
		}
		// Anonymous transfer. Must allocate according to libcanard.
		payloadSize := min(sub.extent, frame.payloadSize)
		payload := make([]byte, payloadSize)
		//rxInitTransferMetadataFromFrame(frame, &out_transfer->metadata);
		outTx.timestamp = frame.timestamp
		outTx.payloadSize = payloadSize
		outTx.payload = payload
		copy(payload, frame.payload[:payloadSize])
	}
	return nil
}

func (ins *Instance) rxSessionUpdate(rxs *InternalRxSession, frame *RxFrameModel, rti uint8, txIdTimeout microsecond, extent int, outTx *RxTransfer) error {
	switch {
	case rxs == nil || frame == nil || outTx == nil:
		return ErrInvalidArgument
	case rxs.tid > TRANSFER_ID_MAX || frame.tid > TRANSFER_ID_MAX:
		return ErrBadTransferID
	}

	TIDTimeOut := frame.timestamp > rxs.txTimestamp && (frame.timestamp-rxs.txTimestamp) > txIdTimeout
	notPreviousTID := rxComputeTransferIDDifference(rxs.tid, frame.tid) > 1
	needRestart := TIDTimeOut || (rxs.rti == rti && frame.txStart && notPreviousTID)

	if needRestart {
		rxs.reset(frame.tid, rti)
	}
	if needRestart && !frame.txStart {
		// SOT miss. Following is equivalent to rxSessionRestart in libcanard
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
	switch {
	case rxs == nil || frame == nil || outTx == nil:
		return ErrInvalidArgument
	case len(frame.payload) == 0:
		return errEmptyPayload
	case frame.tid > TRANSFER_ID_MAX:
		return ErrBadTransferID
	}

	if frame.txStart {
		rxs.txTimestamp = frame.timestamp
	}
	singleFrame := frame.txStart && frame.txEnd
	if !singleFrame {
		rxs.crc = rxs.crc.Add(frame.payload)
	}

	return nil
}

func (ins *Instance) rxSessionWritePayload(rxs *InternalRxSession, extent, payloadSize int, payload []byte) error {
	switch {
	case rxs == nil:
		return ErrInvalidArgument
	case len(payload) == 0 || payloadSize == 0:
		return errEmptyPayload
	case len(rxs.payload) > extent || len(rxs.payload) > rxs.totalPayloadSize:
		//  CANARD_ASSERT((payload != NULL) || (payload_size == 0U)); unreachable in go
		return errTODO
	}

	rxs.totalPayloadSize += payloadSize
	if cap(rxs.payload) == 0 && extent > 0 {
		if rxs.payloadSize != 0 {
			panic("assert rxs.payloadSize == 0")
		}
		// Allocate the payload lazily, as late as possible.
		rxs.payload = make([]byte, extent)
	}
	bytesToCopy := payloadSize
	if rxs.payloadSize+payloadSize > extent {
		bytesToCopy = extent - rxs.payloadSize
		if rxs.payloadSize > extent || rxs.payloadSize+bytesToCopy != extent || bytesToCopy >= payloadSize {
			panic("assert payload bounds rxSessionWritePayload")
		}
	}
	n := copy(rxs.payload[:rxs.payloadSize], payload[:bytesToCopy])
	if n != bytesToCopy {
		panic("insufficient rxs mem")
	}
	rxs.payloadSize += bytesToCopy
	if rxs.payloadSize > extent {
		panic("rxs payload exceed extent")
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
