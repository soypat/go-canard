package canard

import (
	"errors"
	"unsafe"
)

// Contains OpenCyphal receive logic. Exported functions first.

func (ins *Instance) Accept(timestamp microsecond, frame *Frame, rti uint8, outTx *Transfer, outSub *Sub) error {
	switch {
	case ins == nil || outTx == nil || frame == nil:
		return ErrInvalidArgument
	case len(frame.payload) == 0:
		return errEmptyPayload
	}

	model := FrameModel{}
	err := rxTryParseFrame(timestamp, frame, &model)
	if err != nil {
		return err
	}
	if !model.dstNode.IsUnset() && ins.NodeID != model.dstNode {
		return ErrBadDstAddr
	}
	// This is the reason the function has a logarithmic time complexity of the number of subscriptions.
	// Note also that this one of the two variable-complexity operations in the RX pipeline; the other one
	// is memcpy(). Excepting these two cases, the entire RX pipeline contains neither loops nor recursion.
	portCp := model.port
	got, err := search(&ins.rxSub[model.txKind], &portCp, predicateOnPortID, nil)
	if errors.Is(err, ErrAVLNilRoot) || errors.Is(err, ErrAVLNodeNotFound) {
		return ErrNoMatchingSub
	}
	if err != nil {
		return err
	}
	sub := (*Sub)(unsafe.Pointer(got))
	if outSub != nil {
		outSub = sub
	}
	if sub == nil {
		return ErrNoMatchingSub
	}
	if sub.port != model.port {
		return errors.New("TODO sub port not equal to model port")
	}
	return rxAcceptFrame(ins.NodeID, sub, &model, rti, outTx)
}

func (ins *Instance) Subscribe(kind TxKind, port PortID, extent int, tidTimeout microsecond, outSub *Sub) error {
	switch {
	case outSub == nil:
		return ErrInvalidArgument
	case kind >= numberOfTxKinds:
		return ErrTransferKind
	}
	err := ins.Unsubscribe(kind, port)
	if err != nil {
		return err
	}
	outSub.tidTimeout = tidTimeout
	outSub.extent = extent
	outSub.port = port
	got, err := search(&ins.rxSub[kind], outSub, predicateOnStruct, avlTrivialFactory)
	if err != nil {
		return err
	}
	if got != &outSub.base {
		panic("bad search result")
	}
	return nil
}

func (ins *Instance) Unsubscribe(kind TxKind, port PortID) error {
	switch {
	case kind >= numberOfTxKinds:
		return ErrTransferKind
	}
	portcp := port
	got, err := search(&ins.rxSub[kind], &portcp, predicateOnPortID, nil)
	if errors.Is(err, ErrAVLNilRoot) || errors.Is(err, ErrAVLNodeNotFound) {
		return nil // Node not exist, no need to remove.
	}
	if err != nil {
		return err
	}
	sub := (*Sub)(unsafe.Pointer(got))
	if got == nil || sub == nil {
		return nil
	}
	remove(&ins.rxSub[kind], &sub.base)
	if sub.port != port {
		panic("bad search result")
	}
	return nil
}

func (ins *Instance) GetSubs(kind TxKind) (subs []*Sub) {
	switch {
	case kind >= numberOfTxKinds:
		panic("invalid kind")
	}
	ins.rxSub[kind].traverse(0, func(n *TreeNode) {
		sub := (*Sub)(unsafe.Pointer(n))
		subs = append(subs, sub)
	})
	return subs
}

// Below is private API.

type internalRxSession struct {
	txTimestamp      microsecond
	totalPayloadSize int
	payloadSize      int
	payload          []byte
	crc              CRC
	tid              TID
	// Redundant Transport Index
	rti    uint8
	toggle bool
}

func rxSessionWritePayload(rxs *internalRxSession, extent, payloadSize int, payload []byte) error {
	switch {
	case rxs == nil:
		return ErrInvalidArgument
	case len(payload) == 0 && payloadSize != 0:
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

func rxAcceptFrame(dst NodeID, sub *Sub, frame *FrameModel, rti uint8, outTx *Transfer) error {
	switch {
	case sub == nil || frame == nil || outTx == nil:
		return ErrInvalidArgument
	case len(frame.payload) == 0:
		return errEmptyPayload
	case frame.tid > TRANSFER_ID_MAX:
		return ErrBadTransferID
	case !frame.dstNode.IsUnset() && dst != frame.dstNode:
		return ErrBadDstAddr
	case !frame.srcNode.IsValid():
		return ErrInvalidNodeID
	}

	if frame.srcNode <= NODE_ID_MAX {
		// If such session does not exist, create it. This only makes sense if this is the first frame of a
		// transfer, otherwise, we won't be able to receive the transfer anyway so we don't bother.
		if sub.sessions[frame.srcNode] == nil && frame.txStart {
			sub.sessions[frame.srcNode] = &internalRxSession{
				txTimestamp: frame.timestamp,
				crc:         newCRC(),
				tid:         frame.tid,
				rti:         rti,
				toggle:      true, // INITIAL_TOGGLE_STATE
			}
		}
		if sub.sessions[frame.srcNode] != nil {
			return rxSessionUpdate(sub.sessions[frame.srcNode], frame,
				rti, sub.tidTimeout, sub.extent, outTx)
		}
	} else {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func rxSessionUpdate(rxs *internalRxSession, frame *FrameModel, rti uint8, txIdTimeout microsecond, extent int, outTx *Transfer) error {
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
		return rxSessionAcceptFrame(rxs, frame, extent, outTx)
	}
	return errTODO
}

func rxComputeTransferIDDifference(a, b TID) uint8 {
	diff := int16(a) - int16(b)
	if diff < 0 {
		diff += 1 << TRANSFER_ID_BIT_LENGTH
	}
	return uint8(diff)
}

func rxSessionAcceptFrame(rxs *internalRxSession, frame *FrameModel, extent int, outTx *Transfer) error {
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
		rxs.crc = rxs.crc.Add(frame.payload[:frame.payloadSize])
	}
	err := rxSessionWritePayload(rxs, extent, frame.payloadSize, frame.payload)
	if err != nil {
		// OOM session restart here.
		return err
	}
	if !frame.txEnd {
		rxs.toggle = !rxs.toggle
		return errTODO // not sure if this correct
	}
	if !singleFrame && rxs.crc != 0 {
		rxs.reset(rxs.tid+1, rxs.rti)
		return nil
	}
	outTx.metadata.fromRxFrame(frame)
	outTx.timestamp = rxs.txTimestamp
	outTx.payloadSize = rxs.payloadSize
	outTx.payload = rxs.payload
	if rxs.totalPayloadSize < rxs.payloadSize {
		panic("OOB rxs payload")
	}
	truncatedAmount := rxs.totalPayloadSize - rxs.payloadSize
	const CRC_SIZE = 2 // bytes
	if !singleFrame && CRC_SIZE > truncatedAmount {
		if outTx.payloadSize < 2-truncatedAmount {
			panic("OOB crc not fit in payload")
		}
		outTx.payloadSize -= 2 - truncatedAmount
	}
	// Ownership passed over to the application, nullify to prevent modifying.
	rxs.payload = nil
	rxs.reset(rxs.tid+1, rxs.rti)
	return nil
}

func rxTryParseFrame(ts microsecond, frame *Frame, out *FrameModel) error {
	switch {
	case frame == nil || out == nil:
		return ErrInvalidArgument
	case frame.payloadSize == 0:
		return errEmptyPayload
	}

	valid := false
	canID := frame.extendedCANID
	out.timestamp = ts
	out.prority = Priority(canID>>offset_Priority) & priorityMask
	out.srcNode = NodeID(canID & NODE_ID_MAX)
	if canID&FLAG_SERVICE_NOT_MESSAGE == 0 {
		out.txKind = TxKindMessage
		out.port = PortID(canID>>offset_SubjectID) & SUBJECT_ID_MAX
		if canID&FLAG_ANONYMOUS_MESSAGE != 0 {
			out.srcNode.Unset()
		}
		out.dstNode.Unset()
		// Reserved bits may be unreserved in the future.
		valid = ((canID & FLAG_RESERVED_23) == 0) && ((canID & FLAG_RESERVED_07) == 0)
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
		valid = ((canID & FLAG_RESERVED_23) == 0) && (out.srcNode != out.dstNode)
	}

	// Payload parsing.
	out.payloadSize = frame.payloadSize - 1
	out.payload = frame.payload // Cut off the tail byte.

	// Tail byte parsing.
	// No violation of MISRA.
	tail := frame.payload[out.payloadSize]
	out.tid = TID(tail & TRANSFER_ID_MAX)
	out.txStart = (tail & TAIL_START_OF_TRANSFER) != 0
	out.txEnd = (tail & TAIL_END_OF_TRANSFER) != 0
	out.toggle = (tail & TAIL_TOGGLE) != 0

	// Final validation.
	// Protocol version check: if SOT is set, then the toggle shall also be set.
	// valid = valid && ((!out->start_of_transfer) || (INITIAL_TOGGLE_STATE == out->toggle));
	valid = valid && (!out.txStart || out.toggle) //
	// Anonymous transfers can be only single-frame transfers.
	valid = valid && ((out.txStart && out.txEnd) || out.srcNode.IsUnset())
	// Non-last frames of a multi-frame transfer shall utilize the MTU fully.
	valid = valid && ((out.payloadSize >= MFT_NON_LAST_FRAME_PAYLOAD_MIN) || out.txEnd)
	// A frame that is a part of a multi-frame transfer cannot be empty (tail byte not included).
	valid = valid && (out.payloadSize > 0 || (out.txStart && out.txEnd))
	if !valid {
		return errInvalidFrame
	}
	return nil
}

func (rxs *internalRxSession) reset(txid TID, rti uint8) {
	rxs.totalPayloadSize = 0
	rxs.payload = rxs.payload[:0]
	rxs.crc = newCRC()
	rxs.tid = txid & TRANSFER_ID_MAX
	rxs.toggle = true // INITIAL TOGGLE STATE
	rxs.rti = rti
}
