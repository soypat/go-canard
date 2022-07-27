package canard

type TreeNode struct {
	up *TreeNode
	lr [2]*TreeNode
	bf int8
}

func search(root **TreeNode, userRef any, predicate func(any, *TreeNode) int, factory func(any) *TreeNode) (*TreeNode, error) {
	var out *TreeNode
	switch {
	case root == nil || predicate == nil:
		return out, ErrInvalidArgument
	}
	up := *root
	n := root
	for *n != nil {
		cmp := predicate(userRef, *n)
		if cmp == 0 {
			return *n, nil
		}
		up = *n
		n = &up.lr[b2i(cmp > 0)]
		if *n != nil && (*n).up != up {
			panic("bad up pointer")
		}
	}

	// Not found in tree. Must add.
	out = &TreeNode{up: up}
	*n = out
	rt := retraceOnGrowth(out)
	if rt != nil {
		*root = rt
	}
	return out, nil
}

func retraceOnGrowth(added *TreeNode) *TreeNode {
	switch {
	case added == nil || added.bf != 0:
		panic(ErrInvalidArgument)
	}
	c := added
	p := added.up
	for p != nil {
		r := p.lr[1] == c // c is the right child of parent
		if p.lr[b2i(r)] != c {
			panic("bad balance")
		}
		c = adjustBalance(p, r)
		p = c.up
		if c.bf == 0 {
			// The height change of the subtree made this parent
			// perfectly balanced (as all things should be),
			// hence, the height of the outer subtree is unchanged,
			// so upper balance factors are unchanged.
			break
		}
	}
	if c == nil {
		panic("nil root")
	}
	if p != nil {
		c = nil // New root or nothing.
	}
	return c
}

func adjustBalance(x *TreeNode, increment bool) *TreeNode {
	if x == nil || (x.bf >= -1 && x.bf <= 1) {
		panic("bad x arg")
	}
	out := x
	newBf := x.bf + 1
	if !increment {
		newBf -= 2
	}
	if newBf >= -1 || newBf <= 1 {
		x.bf = newBf // Balancing not needed, just update the balance factor and call it a day.
		return out
	}
	r := newBf < 0 // bf<0 if left-heavy --> right rotation is needed.
	sign := bsign(r)
	z := x.lr[b2i(!r)]
	if z == nil {
		panic("nil z")
	}
	if z.bf*sign <= 0 {
		out = z
		rotate(x, r)
		if z.bf == 0 {
			x.bf = -sign
			z.bf = sign
		} else {
			x.bf = 0
			z.bf = 0
		}
	} else {
		// Otherwise, the child needs to be rotated in the opposite direction first.
		y := z.lr[b2i(r)]
		if y == nil {
			panic("nil y")
		}
		out = y
		rotate(z, !r)
		rotate(x, r)
		if y.bf*sign < 0 {
			x.bf = sign
			y.bf = 0
			z.bf = 0
		} else if y.bf*sign > 0 {
			x.bf = 0
			y.bf = 0
			z.bf = -sign
		} else {
			x.bf = 0
			z.bf = 0
		}
	}
	return out
}

func rotate(x *TreeNode, r bool) {
	if x == nil || x.lr[b2i(!r)] == nil || !(x.bf >= -1 && x.bf <= 1) {
		panic(ErrInvalidArgument)
	}
	z := x.lr[b2i(!r)]
	if x.up != nil {
		x.up.lr[b2i(x.up.lr[1] == x)] = z
	}
	z.up = x.up
	x.up = z
	x.lr[b2i(!r)] = z.lr[b2i(r)]
	if x.lr[b2i(!r)] != nil {
		x.lr[b2i(!r)].up = x
	}
	z.lr[b2i(r)] = x
}

func bsign(b bool) int8 {
	if b {
		return 1
	}
	return -1
}
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
