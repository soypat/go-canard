package canard

import "unsafe"

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

func predicateOnPortID(userRef any, node *TreeNode) int8 {
	sought := userRef.(PortID)
	other := (*RxSub)(unsafe.Pointer(node)).port
	if sought == other {
		return 0
	}
	return bsign(sought > other)
}

func predicateOnStruct(userRef any, node *TreeNode) int8 {
	port := userRef.(*RxSub).port
	return predicateOnPortID(port, node)
}
