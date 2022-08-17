package canard

import "testing"

func TestInstanceSingleAndMultiTx(t *testing.T) {
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
	if que.size != 1 || que.root.Height() != 1 {
		t.Error("should have one item on queue")
	}

	// Multiframe transfer
	meta.Priority = PriorityLow
	meta.TID = 22
	que.MTU = _MTU_CAN_CLASSIC
	source = 42
	err = que.Push(source, timeout+1, &meta, 8, payload)
	if err != nil {
		t.Fatal(err)
	}
	if que.size != 3 || que.root.Height() != 3 {
		t.Fatal("size expected to be 3 after 1 single and 2 multi frames")
	}
	// Remove first item and validate it is the first item pushed onto queue.
	if got != que.Pop(nil) {
		t.Fatal("Pop should pop first item added to list due to higher priority")
	}
	multi1 := que.Pop(nil)
	multi2 := que.Pop(nil)
	if multi1 == nil || multi2 == nil {
		t.Fatal("got nil multiframe")
	}
	if que.size != 0 || que.root.Height() != 0 {
		t.Error("TxQueue size not correctly decremented")
	}
	if multi1.deadline != timeout+1 || multi2.deadline != timeout+1 {
		t.Error("timeout not set correctly in multiframe")
	}
	if multi1.frame.payloadSize != 8 || multi2.frame.payloadSize != 4 {
		t.Error("multiframe payload size not set correctly, got", multi1.frame.payloadSize, multi2.frame.payloadSize)
	}
	tail1 := multi1.TailByte()
	if !tail1.IsStart() || tail1.IsEnd() || !tail1.IsToggled() {
		t.Errorf("multiframe1 tail byte incorrect, got %b", tail1)
	}
	tail2 := multi2.TailByte()
	if tail2.IsStart() || !tail2.IsEnd() || tail2.IsToggled() {
		t.Errorf("multiframe2 tail byte incorrect, got %b", tail1)
	}
}
