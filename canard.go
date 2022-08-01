package canard

import (
	"errors"
	"unsafe"
)

type Instance struct {
	userRef any
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
	MTU     int
	size    int
	root    *TreeNode
	userRef any
}
type TxQueueItem struct {
	// Must be first field due to use of unsafe.
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

//go:inline
func (n NodeID) IsValid() bool {
	return n.IsSet() || n.IsUnset()
}

//go:inline
func (n NodeID) IsUnset() bool {
	const unsetNodeID = 0xff // 255
	return n == unsetNodeID
}

//go:inline
func (n *NodeID) Unset() {
	*n = 0xff
}

//go:inline
func (n NodeID) IsSet() bool {
	return n <= NODE_ID_MAX
}

type PortID uint32

// Transfer ID
type TID uint8

/// High-level transport frame model.
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
	got, err := search(&ins.rxSub[model.txKind], model.port, predicateOnPortID, nil)
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
	if sub.port == model.port {
		return errors.New("TODO sub port not equal to model port")
	}
	return ins.rxAcceptFrame(sub, &model, rti, outTx)
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
		panic("multiframe transfer unsupported as of yet")
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
