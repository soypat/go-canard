package canard

import (
	"errors"
	"strconv"
)

var (
	ErrInvalidArgument = errors.New("invalid arg")
	errEmptyPayload    = errors.New("empty or nil payload")
	errInvalidFrame    = errors.New("invalid frame")
	ErrBadDstAddr      = errors.New("bad destination address on frame")
	ErrInvalidNodeID   = errors.New("node id must be in 0.." + strconv.FormatUint(NODE_ID_MAX, 10))
	ErrNoMatchingSub   = errors.New("no matching subscription")
	ErrBadTransferID   = errors.New("transfer id must be in 0.." + strconv.FormatUint(TRANSFER_ID_MAX, 10))
	errTODO            = errors.New("go-canard: generic error")
	ErrTransferKind    = errors.New("undefined transfer kind")

	ErrAVLNodeNotFound = errors.New("avl: node not found")
	ErrAVLNilRoot      = errors.New("avl: nil root")
)
