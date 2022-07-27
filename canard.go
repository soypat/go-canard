package canard

import (
	"errors"
	"unsafe"
)

type Instance struct {
	userRef interface{}
	NodeID  NodeID
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
	// must be first field due to use of unsafe.
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
	if NodeIDUnset != model.dstNode && ins.NodeID != model.dstNode {
		return ErrBadDstAddr
	}
	// This is the reason the function has a logarithmic time complexity of the number of subscriptions.
	// Note also that this one of the two variable-complexity operations in the RX pipeline; the other one
	// is memcpy(). Excepting these two cases, the entire RX pipeline contains neither loops nor recursion.
	// GetRxSub
	got, err := search(&ins.rxSub[model.txKind], model.port, predicateOnPortID, nil)
	if err != nil {
		return err
	}
	sub := (*RxSub)(unsafe.Pointer(got))
	if outSub != nil {
		outSub = sub
	}
	if sub == nil {
		return ErrNoMatchingSub
	}
	if sub.port == model.port {
		return errors.New("TODO sub port not equal to model port")
	}
	return ins.rxAcceptFrame(sub, &model, rti, outTx)
}

func (ins *Instance) rxAcceptFrame(sub *RxSub, frame *RxFrameModel, rti uint8, outTx *RxTransfer) error {
	switch {
	case sub == nil || frame == nil || outTx == nil:
		return ErrInvalidArgument
	case len(frame.payload) == 0:
		return errEmptyPayload
	case frame.tid > TRANSFER_ID_MAX:
		return ErrBadTransferID
	case NodeIDUnset != frame.dstNode && ins.NodeID != frame.dstNode:
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
