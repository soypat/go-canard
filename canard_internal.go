package canard

import "unsafe"

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

func (ins *Instance) rxSessionWritePayload(rxs *internalRxSession, extent, payloadSize int, payload []byte) error {
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

func (ins *Instance) rxAcceptFrame(sub *Sub, frame *FrameModel, rti uint8, outTx *Transfer) error {
	switch {
	case sub == nil || frame == nil || outTx == nil:
		return ErrInvalidArgument
	case len(frame.payload) == 0:
		return errEmptyPayload
	case frame.tid > TRANSFER_ID_MAX:
		return ErrBadTransferID
	case !frame.dstNode.IsUnset() && ins.NodeID != frame.dstNode:
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
			return ins.rxSessionUpdate(sub.sessions[frame.srcNode], frame,
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

func (ins *Instance) rxSessionUpdate(rxs *internalRxSession, frame *FrameModel, rti uint8, txIdTimeout microsecond, extent int, outTx *Transfer) error {
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

func rxComputeTransferIDDifference(a, b TID) uint8 {
	diff := int16(a) - int16(b)
	if diff < 0 {
		diff += 1 << TRANSFER_ID_BIT_LENGTH
	}
	return uint8(diff)
}

func (ins *Instance) rxSessionAcceptFrame(rxs *internalRxSession, frame *FrameModel, extent int, outTx *Transfer) error {
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

func rxTryParseFrame(ts microsecond, frame *Frame, out *FrameModel) error {
	switch {
	case frame == nil || out == nil:
		return ErrInvalidArgument
	case frame.payloadSize == 0:
		return errEmptyPayload
	}

	valid := false
	canID := frame.extendedCANID
	out.prority = Priority(canID>>offset_Priority) & priorityMask
	out.srcNode = NodeID(canID & NODE_ID_MAX)
	if 0 == canID&FLAG_SERVICE_NOT_MESSAGE {
		out.txKind = TxKindMessage
		out.port = PortID(canID>>offset_SubjectID) & SUBJECT_ID_MAX
		if canID&FLAG_ANONYMOUS_MESSAGE != 0 {
			out.srcNode.Unset()
		}
		out.dstNode.Unset()
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
	tail := frame.payload[out.payloadSize]
	out.tid = TID(tail & TRANSFER_ID_MAX)
	out.txStart = (tail & TAIL_START_OF_TRANSFER) != 0
	out.txEnd = (tail & TAIL_END_OF_TRANSFER) != 0
	out.toggle = (tail & TAIL_TOGGLE) != 0

	// Final validation.
	// Protocol version check: if SOT is set, then the toggle shall also be set.
	// valid = valid && ((!out->start_of_transfer) || (INITIAL_TOGGLE_STATE == out->toggle));
	valid = valid && (!out.txStart || true == out.toggle) //
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
	rxs.tid = txid
	rxs.toggle = true // INITIAL TOGGLE STATE
	rxs.rti = rti
}

func predicateOnPortID(userRef any, node *TreeNode) int8 {
	sought := *userRef.(*PortID)
	other := (*Sub)(unsafe.Pointer(node)).port
	if sought == other {
		return 0
	}
	return bsign(sought > other)
}

func predicateOnStruct(userRef any, node *TreeNode) int8 {
	port := userRef.(*Sub).port
	return predicateOnPortID(port, node)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func adjustPresentationLayerMTU(mtuBytes int) (mtu int) {
	switch {
	case mtuBytes < _MTU_CAN_CLASSIC:
		mtu = _MTU_CAN_CLASSIC
	case mtuBytes <= len(canLengthToDLC)-1:
		mtu = int(canDLCToLength[canLengthToDLC[mtuBytes]])
	default:
		mtu = int(canDLCToLength[canLengthToDLC[len(canLengthToDLC)-1]])
	}
	return mtu - 1
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
		} else if payloadSize < presentationLayerMTU {
			c := newNodeID(payload[:payloadSize])
			out = makeMessageSessionSpecifier(m.Port, c) | FLAG_ANONYMOUS_MESSAGE
			if out < _CAN_EXT_ID_MASK {
				panic("out < can_ext_id_mask TODO")
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

func (q *TxQueue) pushSingleFrame(deadline microsecond, canID uint32, tid TID, payloadSize int, payload []byte) error {
	framePayloadSize := roundPayloadSizeUp(payloadSize + 1)
	padding := framePayloadSize - payloadSize - 1
	switch {
	case len(payload) == 0 && payloadSize != 0:
		return errEmptyPayload
	case framePayloadSize <= payloadSize:
		panic("OOB framePayloadSize <= payloadSize")
	case padding+payloadSize+1 != framePayloadSize:
		panic("overflow")
	}
	tqi := newTxItem(deadline, payloadSize, canID)
	if payloadSize > 0 {
		copy(tqi.payloadBuffer[:], payload[:payloadSize])
	}
	// Set tail byte.
	tqi.payloadBuffer[framePayloadSize-1] = tailByte(true, true, true, tid)
	res, err := search(&q.root, tqi.base.base, predicateTx, avlTrivialFactory)
	if err != nil {
		return err
	}
	if res != &tqi.base.base {
		panic("bad AVL search result")
	}
	q.size++
	if q.size > q.Cap {
		panic("OOM on queue size exceed cap")
	}
	return nil
}

func roundPayloadSizeUp(x int) int {
	return int(canDLCToLength[canLengthToDLC[x]])
}

func tailByte(start, end, toggle bool, tid TID) (tail byte) {
	tail = byte(tid & TRANSFER_ID_MAX)
	tail |= byte(b2i(toggle) << 5)
	tail |= byte(b2i(end) << 6)
	tail |= byte(b2i(start) << 7)
	return tail
}

func predicateTx(userRef any, node *TreeNode) int8 {
	target := userRef.(*TxQueueItem)
	other := (*TxQueueItem)(unsafe.Pointer(node))
	sign := bsign(target.frame.extendedCANID >= other.frame.extendedCANID)
	return sign
}

func newTxItem(deadline microsecond, size int, extendedCANID uint32) *TxItem {
	tqi := &TxItem{
		base: TxQueueItem{
			deadline: deadline,
			frame: Frame{
				payloadSize:   size,
				extendedCANID: extendedCANID,
			},
		},
	}
	tqi.base.frame.payload = tqi.payloadBuffer[:]
	return tqi
}

func (root *TreeNode) traverse(i int, fn func(n *TreeNode)) int {
	l, r := root.lr[0], root.lr[1]
	if l != nil {
		fn(l)
		i = l.traverse(i+1, fn)
	}
	if r != nil {
		fn(r)
		i = r.traverse(i+1, fn)
	}
	return i
}
