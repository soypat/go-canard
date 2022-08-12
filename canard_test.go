package canard

import (
	"errors"
	"testing"
)

func TestInstanceSubscribe(t *testing.T) {
	const (
		extendedCANID = 0b001_00_0_11_0110011001100_0_0100111
		payloadSize   = 65
	)
	ins, _, sub, accept := newInstanceHelper()
	subCp := *sub
	err := accept(0, 1000e6, extendedCANID, []byte{tailByte(true, true, true, 0)})
	if !errors.Is(err, ErrNoMatchingSub) {
		t.Fatalf("expecting ErrNoMatchingSub, got %v", err)
	}
	if *sub != subCp {
		t.Error("expected subscription to remain equal after accepting")
	}

	// Create a message subscription.
	subMsg := Sub{}
	const portid = 0xccc
	// New.
	err = ins.Subscribe(TxKindMessage, portid, 32, 2e6, &subMsg)
	if err != nil {
		t.Error("expected error on new", err)
	}
	// Replacement should annihilate values written in first call to Subscribe.
	const replacedExtent, replacedTimeout = 16, 1e6
	err = ins.Subscribe(TxKindMessage, portid, replacedExtent, replacedTimeout, &subMsg)
	if err != nil {
		t.Error(err)
	}
	subs := ins.GetSubs(TxKindMessage)
	if len(subs) != 1 {
		t.Fatal("expected single subscription, got", len(subs))
	}
	got := subs[0]
	if got != &subMsg {
		t.Error("subMsg pointer incorrectly set")
	}
	if got.port != portid {
		t.Errorf("wrong Subscription portid. got %v, expected %v", got.port, portid)
	}
	if got.extent != replacedExtent {
		t.Error("extent replacement failed")
	}
	if got.tidTimeout != replacedTimeout {
		t.Error("timeout replacement failed")
	}
	for _, ptr := range got.sessions {
		if ptr != nil {
			t.Error("all sessions should be uninitialized, got ", *ptr)
		}
	}

	// Create request subscription.
	subReq := &Sub{}
	const reqPort, reqExtent, reqTimeout = 0b0000110011, 20, 3e6
	err = ins.Subscribe(TxKindRequest, reqPort, reqExtent, reqTimeout, subReq)
	if err != nil {
		t.Error(err)
	}
	// Check message su
	subs = ins.GetSubs(TxKindMessage)
	if len(subs) != 1 {
		t.Fatal("expected single message subscription, got", len(subs))
	}
	got = subs[0]
	if got != &subMsg {
		t.Error("subMsg pointer incorrectly set")
	}
	subs = ins.GetSubs(TxKindRequest)
	if len(subs) != 1 {
		t.Fatal("expected single request subscription, got", len(subs))
	}
	got = subs[0]
	if got != subReq {
		t.Error("subReq pointer incorrectly set")
	}
	if got.extent != reqExtent || got.port != reqPort || got.tidTimeout != reqTimeout {
		t.Error("got request value not set correctly, got", *got)
	}
}

func TestInstanceAccept(t *testing.T) {
	const (
		extendedCANID ecID = 0b001_00_0_11_0110011001100_0_0100111
		port               = 0xccc
		payloadSize        = 65
		timeout            = 1e8 + 1
	)
	ins, transfer, sub, accept := newInstanceHelper()
	// Create a new subscription on which to listen.
	err := ins.Subscribe(TxKindMessage, port, 16, timeout, sub)
	if err != nil {
		t.Fatal(err)
	}
	// Send
	err = accept(0, timeout, uint32(extendedCANID), []byte{tailByte(true, true, true, 0)})
	if err != nil {
		t.Fatal(err)
	}
	_ = transfer
	if sub.port != extendedCANID.PortID() {
		t.Error("port ID not expected", extendedCANID.PortID(), sub.port)
	}
	if transfer.timestamp != timeout {
		t.Error("transfer timeout not set", transfer)
	}
	if transfer.metadata.TxKind != TxKindMessage {
		t.Error("txkind expected to be mesage")
	}
}
