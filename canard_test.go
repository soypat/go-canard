package canard

import "testing"

func TestRxBasic(t *testing.T) {
	const (
		extendedCANID = 0b001_00_0_11_0110011001100_0_0100111
		payloadSize   = 65
	)
	ins := &Instance{}
	transfer := &RxTransfer{}
	var sub *RxSub
	accept := func(rti uint8, timestamp microsecond, canid uint32, payload []byte) error {
		return ins.Accept(timestamp, &Frame{
			extendedCANID: canid,
			payloadSize:   len(payload),
			payload:       payload,
		}, rti, transfer, sub)
	}
	err := accept(0, 1000e6, extendedCANID, []byte{0b111_00000})
	if err != nil {
		t.Fatal(err)
	}
	if sub != nil {
		t.Error("expected subscription to remain nil after accepting")
	}
	subMsg := RxSub{}
	// New.
	err = ins.Subscribe(TxKindMessage, 0b0110011001100, 32, 2e6, &subMsg)
	if err != nil {
		t.Error("expected error on new", err)
	}
	// Replaced
	err = ins.Subscribe(TxKindMessage, 0b0110011001100, 16, 1e6, &subMsg)
	if err != nil {
		t.Error(err)
	}
}
