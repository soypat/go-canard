package canard

import (
	"math"
	"unsafe"
)

// Contains OpenCyphal transmission/transfer logic.

func (q *TxQueue) Push(src NodeID, txDeadline microsecond, metadata *Metadata, payloadSize int, payload []byte) error {
	switch {
	case q == nil || metadata == nil:
		return ErrInvalidArgument
	case len(payload) == 0 && payloadSize != 0:
		return errEmptyPayload
	}
	pl_mtu := adjustPresentationLayerMTU(q.MTU)
	maybeCan, err := metadata.makeCANID(payloadSize, payload, src, pl_mtu)
	if err != nil {
		return err
	}
	if payloadSize > pl_mtu {
		_, err := q.pushMultiFrame(txDeadline, maybeCan, metadata.TID, pl_mtu, payloadSize, payload)
		return err
	}
	err = q.pushSingleFrame(txDeadline, maybeCan, metadata.TID, payloadSize, payload)
	if err != nil {
		return err
	}
	return nil
}

func (q *TxQueue) Peek() *TxQueueItem {
	tqi := findExtremum(q.root, false)
	if tqi == nil {
		return nil
	}
	return (*TxQueueItem)(unsafe.Pointer(tqi))
}

// Pop removes item from the TxQueue and returns the removed item.
// If item is nil then the first item is removed from the Queue and returned.
func (q *TxQueue) Pop(item *TxQueueItem) *TxQueueItem {
	if item == nil {
		item = q.Peek()
		if item == nil {
			panic("attempted to pop with no items in queue")
		}
	}
	remove(&q.root, &item.base)
	q.size--
	return item
}

// / Chain of TX frames prepared for insertion into a TX queue.
type txChain struct {
	head *TxItem
	tail *TxItem
	size int
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

func (q *TxQueue) pushMultiFrame(deadline microsecond, canID uint32, tid TID, pl_mtu, payloadSize int, payload []byte) (int, error) {
	switch {
	case len(payload) == 0 && payloadSize != 0:
		return 0, errEmptyPayload
	}
	// Number of frames enqueued.
	var out int
	payloadSizeWithCRC := payloadSize + int(unsafe.Sizeof(CRC(0)))
	numFrames := (payloadSizeWithCRC + pl_mtu - 1) / pl_mtu
	if numFrames < 2 {
		panic("unreachable: pushMultiframe used for single frame transport")
	}
	if q.size+numFrames > q.Cap {
		panic("OOM: tx queue capacity cannot fit multiframe transport")
	}
	sq := generateMultiFrameChain(deadline, canID, tid, pl_mtu, payloadSize, payload)
	if sq.tail == nil {
		panic("nil tail")
	} else if sq.head == nil {
		panic("nil head")
	}
	next := &sq.head.base
	for next != nil {
		res, err := search(&q.root, &next.base, predicateTx, avlTrivialFactory)
		if err != nil {
			return 0, err
		}
		if res != &next.base || q.root == nil {
			panic("bad search result")
		}
		next = next.nextInTx
	}
	if numFrames != sq.size || q.size > q.Cap || sq.size > math.MaxInt32 {
		panic("unexpected result in multi frame transfer")
	}
	q.size += sq.size
	out = sq.size
	return out, nil
}

func generateMultiFrameChain(deadline microsecond, canID uint32, tid TID, pl_mtu, payloadSize int, payload []byte) txChain {
	switch {
	case pl_mtu <= 0:
		panic("bad presentation layer MTU")
	case payloadSize <= pl_mtu:
		panic("multi frame needs smaller than MTU payload size")
	}
	const crcSize = int(unsafe.Sizeof(CRC(0)))
	payloadWithOffset := payload
	var chain txChain
	payloadSizeWithCRC := payloadSize + crcSize
	crc := newCRC().Add(payload[:payloadSize])
	toggle := true // initial toggle state
	offset := 0
	for offset < payloadSizeWithCRC {
		var frameWithTailSize int
		chain.size++
		if payloadSizeWithCRC-offset < pl_mtu {
			frameWithTailSize = roundPayloadSizeUp(payloadSizeWithCRC - offset + 1)
		} else {
			frameWithTailSize = pl_mtu + 1
		}
		tqi := newTxItem(deadline, frameWithTailSize, canID)
		if chain.head == nil {
			chain.head = tqi
		} else {
			chain.tail.base.nextInTx = &tqi.base
		}
		chain.tail = tqi

		// copy payload into the frame
		framePayloadSize := frameWithTailSize - 1
		frameOffset := 0
		if offset < payloadSize {
			moveSize := payloadSize - offset
			if moveSize > framePayloadSize {
				moveSize = framePayloadSize
			}
			copy(chain.tail.payloadBuffer[:], payloadWithOffset[:moveSize])
			frameOffset += moveSize
			offset += moveSize
			payloadWithOffset = payloadWithOffset[moveSize:]
		}

		if offset >= payloadSize {
			// Handle the last frame of transfer. Contains padding and CRC.
			for frameOffset+crcSize < framePayloadSize {
				// Insert padding for last frame and include it in CRC.
				chain.tail.payloadBuffer[frameOffset] = 0 // Padding value.
				frameOffset++
				crc = crc.AddByte(0)
			}

			if frameOffset < framePayloadSize && offset == payloadSize {
				// Insert higher bits of CRC.
				chain.tail.payloadBuffer[frameOffset] = byte(crc >> 8)
				frameOffset++
				offset++
			}
			if frameOffset < framePayloadSize && offset > payloadSize {
				// Insert lower bits of CRC.
				chain.tail.payloadBuffer[frameOffset] = byte(crc & 0xff)
				frameOffset++
				offset++
			}
		}

		if frameOffset+1 != chain.tail.base.frame.payloadSize {
			panic("frameOffset+1 != tail frame payloadsize")
		}
		chain.tail.payloadBuffer[frameOffset] = tailByte(chain.head == chain.tail, offset >= payloadSizeWithCRC, toggle, tid)
		toggle = !toggle
	}
	return chain
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
	tqi := newTxItem(deadline, framePayloadSize, canID)
	if payloadSize > 0 {
		copy(tqi.payloadBuffer[:], payload[:payloadSize])
	}
	// Set tail byte.
	tqi.payloadBuffer[framePayloadSize-1] = tailByte(true, true, true, tid)
	res, err := search(&q.root, &tqi.base.base, predicateTx, avlTrivialFactory)
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
