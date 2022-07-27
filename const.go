package canard

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

const (
	offset_Priority  = 26
	offset_SubjectID = 8
	offset_ServiceID = 14
	offset_DstNodeID = 7
)
