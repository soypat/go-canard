package canard

const (
	nodeIDUnset = 255
)

type TxKind uint8

const (
	TxKindMessage  TxKind = iota ///< Multicast, from publisher to all subscribers.
	TxKindResponse               ///< Point-to-point, from server to client.
	TxKindRequest                ///< Point-to-point, from client to server.
)

type Priority uint8

// Transfer priority level mnemonics per the recommendations given in the Cyphal Specification.
const (
	PriorityExceptional Priority = iota
	PriorityImmediate
	PriorityFast
	PriorityHigh
	PriorityNominal // Nominal priority level should be the default.
	PriorityLow
	PrioritySlow
	PriorityOptional
	numOfPriorities
	priorityMask = 0b111
)

/// Parameter ranges are inclusive; the lower bound is zero for all. See Cyphal/CAN Specification for background.
const (
	SUBJECT_ID_MAX         = 8191
	SERVICE_ID_MAX         = 511
	NODE_ID_MAX            = 127
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
	TAIL_START_OF_TRANSFER         = 128
	TAIL_END_OF_TRANSFER           = 64
	TAIL_TOGGLE                    = 32
	MFT_NON_LAST_FRAME_PAYLOAD_MIN = 7
)

const (
	offset_Priority  = 26
	offset_SubjectID = 8
	offset_ServiceID = 14
	offset_DstNodeID = 7
)

const (
	_CAN_EXT_ID_MASK = 1<<29 - 1
	_MTU_CAN_CLASSIC = 8
	_MTU_CAN_FD      = 64
	_MTU_MAX         = _MTU_CAN_FD
)

var (
	canDLCToLength = [16]uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 12, 16, 20, 24, 32, 48, 64}
	canLengthToDLC = [65]uint8{
		0, 1, 2, 3, 4, 5, 6, 7, 8, // 0-8
		9, 9, 9, 9, // 9-12
		10, 10, 10, 10, // 13-16
		11, 11, 11, 11, // 17-20
		12, 12, 12, 12, // 21-24
		13, 13, 13, 13, 13, 13, 13, 13, // 25-32
		14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, // 33-48
		15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, // 49-64
	}
)
