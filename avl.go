package canard

// This package defines AVL tree
// data structure and algorithms.

// TreeNode is a node of an AVL tree.
type TreeNode struct {
	up *TreeNode
	lr [2]*TreeNode
	// Balance factor.
	bf int8
}

func search(root **TreeNode, userRef any, predicate func(any, *TreeNode) int8, factory func(any) *TreeNode) (*TreeNode, error) {
	var out *TreeNode
	switch {
	case root == nil || predicate == nil:
		return out, ErrInvalidArgument
	case *root == nil && factory == nil:
		// Edge case copied from libcanard.
		return out, ErrAVLNilRoot
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
	if factory == nil {
		return nil, ErrAVLNodeNotFound
	}

	// Not found in tree. Must add.
	out = factory(userRef)
	*n = out
	out.up = up
	out.lr = [2]*TreeNode{}
	out.bf = 0
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
	if x == nil || !(x.bf >= -1 && x.bf <= 1) {
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

func findExtremum(root *TreeNode, max bool) *TreeNode {
	var result *TreeNode
	r := b2i(max)
	c := root
	for c != nil {
		result = c
		c = c.lr[r]
	}
	return result
}

func remove(root **TreeNode, node *TreeNode) {
	if root == nil || node == nil {
		return
	}
	if *root == nil || !(node.up != nil || node == *root) {
		panic(ErrInvalidArgument)
	}
	var p *TreeNode // The lowest parent node that suffered a shortening of its subtree.
	r := b2i(false) // Which side of the above was shortened.
	// The first step is to update the topology and remember the node where to start the retracing from later.
	// Balancing is not performed yet so we may end up with an unbalanced tree.
	if node.lr[0] != nil && node.lr[1] != nil {
		re := findExtremum(node.lr[1], false)
		if re == nil || re.lr[0] == nil || re.up == nil {
			panic("invalid re extremum")
		}
		re.bf = node.bf
		re.lr[0] = node.lr[0]
		re.lr[0].up = re
		if re.up != node {
			p = re.up // Retracing starts with the ex-parent of our replacement node.
			if p.lr[0] != re {
				panic("todo bad re")
			}
			p.lr[0] = re.lr[1] // Reducing the height of the left subtree here.
			if p.lr[0] != nil {
				p.lr[0].up = p
			}
			re.lr[1] = node.lr[1]
			re.lr[1].up = re
			r = 0
		} else {
			// In this case, we are reducing the height of the right subtree, so r=1.
			p = re // Retracing starts with the replacement node itself as we are deleting its parent.
			r = 1  // The right child of the replacement node remains the same so we don't bother relinking it.
		}
		re.up = node.up
		if re.up != nil {
			re.up.lr[b2i(re.up.lr[1] == node)] = re // Replace link in the parent of node.
		} else {
			*root = re
		}
	} else {
		p = node.up
		rr := b2i(node.lr[1] != nil)
		if node.lr[rr] != nil {
			node.lr[rr].up = p
		}
		if p != nil {
			r = b2i(p.lr[1] == node)
			p.lr[r] = node.lr[rr]
			if p.lr[r] != nil {
				p.lr[r].up = p
			}
		} else {
			*root = node.lr[rr]
		}
	}
	if p == nil {
		return // work is done.
	}
	// Now that the topology is updated, perform the retracing to restore balance. We climb up adjusting the
	// balance factors until we reach the root or a parent whose balance factor becomes plus/minus one, which
	// means that that parent was able to absorb the balance delta; in other words, the height of the outer
	// subtree is unchanged, so upper balance factors shall be kept unchanged.
	var c *TreeNode
	for {
		c = adjustBalance(p, !(r == 1))
		p = c.up
		if c.bf != 0 || p == nil {
			// Reached the root or the height difference is absorbed by c.
			break
		}
		r = b2i(p.lr[1] == c)
	}
	if p == nil {
		if c == nil {
			panic("nil c at end of remove")
		}
		*root = c
	}
}

/// Used for inserting new items into AVL trees.
func avlTrivialFactory(userRef any) (node *TreeNode) {
	switch a := userRef.(type) {
	case *Sub:
		node = &a.base
	case *TreeNode:
		node = a
	default:
		panic("undefined type")
	}
	return node
}

// Height is a recursive function to find node height.
// Best not used in production.
func (n *TreeNode) Height() int {
	if n == nil {
		return 0
	}
	return 1 + max(n.lr[0].height(0), n.lr[1].height(0))
}

func (n *TreeNode) height(lvl int) int {
	// Allow max 12 levels. Probably a loop if reached.
	if n == nil || lvl == 11 {
		return lvl
	}
	return max(n.lr[0].height(lvl+1), n.lr[1].height(lvl+1))
}

func (n *TreeNode) Len() (out int) {
	n.traverse(0, func(n *TreeNode) {
		out++
	})
	return out + 1
}

//go:inline
func bsign(b bool) int8 {
	if b {
		return 1
	}
	return -1
}

//go:inline
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func max(a, b int) int {
	if b > a {
		return b
	}
	return a
}

func (root *TreeNode) traverse(i int, fn func(n *TreeNode)) int {
	fn(root)
	l, r := root.lr[0], root.lr[1]
	if l != nil {
		i = l.traverse(i+1, fn)
	}
	if r != nil {
		i = r.traverse(i+1, fn)
	}
	return i
}
