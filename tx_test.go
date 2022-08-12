package canard

import "testing"

func TestInstanceTx(t *testing.T) {
	const (
		timeout = 1e8
		tid     = 21
	)
	var source NodeID
	source.Unset()
	que := TxQueue{
		Cap: 200,
		MTU: _MTU_CAN_FD,
	}

	payload := make([]byte, 1024)
	for i := 0; i < len(payload); i++ {
		payload[i] = byte(i & 0xff)
	}

	meta := Metadata{
		Priority: PriorityNominal,
		TxKind:   TxKindMessage,
		Port:     321,
		Remote:   0xff, //unset node id
		TID:      tid,
	}
	err := que.Push(source, timeout, &meta, 8, payload)
	if err != nil {
		t.Fatal(err)
	}
	got := que.Peek()
	if got.deadline != timeout {
		t.Error("deadline incorrectly set")
	}
	if got.frame.payloadSize != 12 {
		t.Error("padding not correctly formed")
	}
	gotPayload := got.frame.payload[:got.frame.payloadSize]
	for i, b := range gotPayload {
		if i < 8 && b&0xff != byte(i) {
			t.Error("mismatch in payload at", i)
		} else if i >= 8 && i < 11 && b != 0 {
			t.Error("padding must be 0")
		} else if i == got.frame.payloadSize-1 && b != tailByte(true, true, true, tid) {
			t.Error("wrong tailbyte bits, got", b)
		}
	}
	meta.Priority = PriorityLow
	meta.TID = 22
	que.MTU = _MTU_CAN_CLASSIC
	source = 42
	err = que.Push(source, timeout+1, &meta, 8, payload)
	if err != nil {
		t.Fatal(err)
	}
}
