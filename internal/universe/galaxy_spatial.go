package universe

import (
	"math"
	"sort"
)

type galaxyPoint struct {
	star            Star
	slot, component int
}
type galaxyTree struct {
	point                  galaxyPoint
	left, right            *galaxyTree
	minX, minY, maxX, maxY float64
	uniform                int
}

func galaxyTreeFor(stars []Star) *galaxyTree {
	points := make([]galaxyPoint, len(stars))
	for i, s := range stars {
		points[i] = galaxyPoint{star: s, slot: i}
	}
	return buildGalaxyTree(points, 0)
}
func buildGalaxyTree(points []galaxyPoint, depth int) *galaxyTree {
	if len(points) == 0 {
		return nil
	}
	sort.Slice(points, func(i, j int) bool {
		a, b := points[i].star, points[j].star
		x, y := a.X, b.X
		if depth%2 == 1 {
			x, y = a.Y, b.Y
		}
		if x == y {
			return a.ID < b.ID
		}
		return x < y
	})
	mid := len(points) / 2
	node := &galaxyTree{point: points[mid], left: buildGalaxyTree(points[:mid], depth+1), right: buildGalaxyTree(points[mid+1:], depth+1)}
	p := node.point.star
	node.minX, node.maxX, node.minY, node.maxY = p.X, p.X, p.Y, p.Y
	for _, child := range []*galaxyTree{node.left, node.right} {
		if child != nil {
			node.minX = math.Min(node.minX, child.minX)
			node.minY = math.Min(node.minY, child.minY)
			node.maxX = math.Max(node.maxX, child.maxX)
			node.maxY = math.Max(node.maxY, child.maxY)
		}
	}
	return node
}
func (n *galaxyTree) lowerDistance(x, y float64) float64 {
	if n == nil {
		return math.Inf(1)
	}
	dx, dy := math.Max(0, math.Max(n.minX-x, x-n.maxX)), math.Max(0, math.Max(n.minY-y, y-n.maxY))
	return dx*dx + dy*dy
}
func (n *galaxyTree) mark(find func(int) int) int {
	if n == nil {
		return -1
	}
	n.point.component = find(n.point.slot)
	n.uniform = n.point.component
	for _, child := range []*galaxyTree{n.left, n.right} {
		if child != nil && child.mark(find) != n.point.component {
			n.uniform = -1
		}
	}
	return n.uniform
}

// Uniform component subtrees are pruned during Boruvka's exact Euclidean MST.
func (n *galaxyTree) nearest(s Star, excluded int, maxSquared float64, allowed func(Star) bool) (*galaxyPoint, float64) {
	var best *galaxyPoint
	distance := maxSquared
	var visit func(*galaxyTree)
	visit = func(node *galaxyTree) {
		if node == nil || (excluded >= 0 && node.uniform == excluded) || node.lowerDistance(s.X, s.Y) > distance {
			return
		}
		p := &node.point
		dx, dy := s.X-p.star.X, s.Y-p.star.Y
		d := dx*dx + dy*dy
		if p.star.ID != s.ID && (excluded < 0 || p.component != excluded) && (allowed == nil || allowed(p.star)) && (d < distance || (d == distance && (best == nil || p.star.ID < best.star.ID))) {
			best = p
			distance = d
		}
		a, b := node.left, node.right
		if b.lowerDistance(s.X, s.Y) < a.lowerDistance(s.X, s.Y) {
			a, b = b, a
		}
		visit(a)
		visit(b)
	}
	visit(n)
	return best, distance
}

type galaxyEdge struct {
	a, b     int
	distance float64
	valid    bool
}

func galaxyEdgeKey(a, b int) [2]int {
	if a > b {
		a, b = b, a
	}
	return [2]int{a, b}
}
func edgeLess(a, b galaxyEdge) bool {
	if a.distance != b.distance {
		return a.distance < b.distance
	}
	ak, bk := galaxyEdgeKey(a.a, a.b), galaxyEdgeKey(b.a, b.b)
	if ak[0] != bk[0] {
		return ak[0] < bk[0]
	}
	return ak[1] < bk[1]
}

// Large galaxies use the same shortest-edge forest and nearest bridge rules,
// with indexed neighbour searches instead of all-pairs scans.
func largeGalaxyLanes(stars []Star, d *GalaxyDefinition) []Lane {
	edges := []Lane{}
	seen := map[[2]int]bool{}
	add := func(a, b int) {
		key := galaxyEdgeKey(a, b)
		if a != b && !seen[key] {
			seen[key] = true
			edges = append(edges, Lane{From: key[0], To: key[1]})
		}
	}
	groups := map[int][]Star{}
	arms := [4][]Star{}
	for _, s := range stars {
		if s.Region >= 0 {
			groups[s.Region] = append(groups[s.Region], s)
			arms[s.Region%4] = append(arms[s.Region%4], s)
		}
	}
	regions := make([]int, 0, len(groups))
	for region := range groups {
		regions = append(regions, region)
	}
	sort.Ints(regions)
	limit := float64(d.Settings.MaxLaneLength) * float64(d.Settings.MaxLaneLength)
	for _, region := range regions {
		members := groups[region]
		if len(members) < 2 {
			continue
		}
		tree := galaxyTreeFor(members)
		parent := make([]int, len(members))
		sizes := make([]int, len(members))
		for i := range parent {
			parent[i] = i
			sizes[i] = 1
		}
		var find func(int) int
		find = func(i int) int {
			for i != parent[i] {
				parent[i] = parent[parent[i]]
				i = parent[i]
			}
			return i
		}
		for {
			tree.mark(find)
			best := make([]galaxyEdge, len(members))
			for i, s := range members {
				root := find(i)
				point, dist := tree.nearest(s, root, limit, nil)
				if point == nil {
					continue
				}
				edge := galaxyEdge{a: i, b: point.slot, distance: dist, valid: true}
				if !best[root].valid || edgeLess(galaxyEdge{a: s.ID, b: point.star.ID, distance: dist}, galaxyEdge{a: members[best[root].a].ID, b: members[best[root].b].ID, distance: best[root].distance}) {
					best[root] = edge
				}
			}
			joined := 0
			for _, edge := range best {
				if !edge.valid {
					continue
				}
				a, b := find(edge.a), find(edge.b)
				if a == b {
					continue
				}
				if sizes[a] < sizes[b] {
					a, b = b, a
				}
				parent[b] = a
				sizes[a] += sizes[b]
				joined++
				add(members[edge.a].ID, members[edge.b].ID)
			}
			if joined == 0 {
				break
			}
		}
		for _, s := range members {
			if stablePercent(d.Seed, s.ID, 3) >= d.Settings.ExtraLanes {
				continue
			}
			point, _ := tree.nearest(s, -1, limit, func(p Star) bool { return !seen[galaxyEdgeKey(s.ID, p.ID)] })
			if point != nil {
				add(s.ID, point.star.ID)
			}
		}
	}
	if d.Settings.CrossLinks > 0 {
		keep := (d.Settings.CrossLinks + 3) / 4
		candidates := [4][]galaxyEdge{}
		for arm := 0; arm < 4; arm++ {
			tree := galaxyTreeFor(arms[(arm+1)%4])
			if tree == nil {
				continue
			}
			for _, s := range arms[arm] {
				picked := map[int]bool{}
				for range keep {
					bound := limit
					list := candidates[arm]
					if len(list) == keep {
						bound = math.Min(bound, list[len(list)-1].distance)
					}
					point, dist := tree.nearest(s, -1, bound, func(p Star) bool { return !picked[p.ID] && !seen[galaxyEdgeKey(s.ID, p.ID)] })
					if point == nil {
						break
					}
					picked[point.star.ID] = true
					list = append(list, galaxyEdge{a: s.ID, b: point.star.ID, distance: dist, valid: true})
					sort.Slice(list, func(i, j int) bool { return edgeLess(list[i], list[j]) })
					if len(list) > keep {
						list = list[:keep]
					}
					candidates[arm] = list
				}
			}
		}
		for i := 0; i < d.Settings.CrossLinks; i++ {
			list := candidates[i%4]
			if i/4 < len(list) {
				edge := list[i/4]
				add(edge.a, edge.b)
			}
		}
	}
	return edges
}
