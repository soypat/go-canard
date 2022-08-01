package canard

func newInstanceHelper() (ins *Instance, t *Transfer, sub *Sub, accept func(rti uint8, timestamp microsecond, canid uint32, payload []byte) error) {
	ins = &Instance{}
	t = &Transfer{}
	sub = &Sub{}
	accept = func(rti uint8, timestamp microsecond, canid uint32, payload []byte) error {
		return ins.Accept(timestamp, &Frame{
			extendedCANID: canid,
			payloadSize:   len(payload),
			payload:       payload,
		}, rti, t, sub)
	}
	return ins, t, sub, accept
}
