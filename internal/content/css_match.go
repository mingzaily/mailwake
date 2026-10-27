package content

import "golang.org/x/net/html"

const maxCSSMatchWork = 2_000_000

type cssElement struct {
	source, clean    *html.Node
	parent, previous int
	id               string
	classes          map[string]bool
}

// Index the parsed source tree, including elements later removed or unwrapped.
// Parent and previous-element indices precede each node in document order.
func (s *bodySanitizer) indexElements(doc *html.Node) error {
	s.elementIndex = make(map[*html.Node]int)
	s.selectors = make(map[string]string)
	nodes := 0
	var visit func(*html.Node, int, int, int) error
	visit = func(n *html.Node, parent, previous, depth int) error {
		nodes++
		if depth > 256 || nodes > 20000 {
			return errHTMLLimit
		}
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if n.Type == html.ElementNode {
			classes := cssFields(firstAttribute(n, "class"))
			if len(classes) > 32 {
				return errHTMLLimit
			}
			e := cssElement{source: n, parent: parent, previous: previous, id: firstAttribute(n, "id")}
			if len(classes) > 0 {
				e.classes = make(map[string]bool, len(classes))
				for _, class := range classes {
					e.classes[class] = true
				}
			}
			parent = len(s.elements)
			s.elementIndex[n] = parent
			s.elements = append(s.elements, e)
		}
		previous = -1
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := visit(c, parent, previous, depth+1); err != nil {
				return err
			}
			if c.Type == html.ElementNode {
				previous = s.elementIndex[c]
			}
		}
		return nil
	}
	return visit(doc, -1, -1, 0)
}

func (c cssCompound) matches(e cssElement) bool {
	if c.root && e.parent != -1 {
		return false
	}
	if c.tag != "" && c.tag != "*" && (e.source.Namespace != "" || c.tag != e.source.Data) {
		return false
	}
	for _, id := range c.ids {
		if id != e.id {
			return false
		}
	}
	for _, class := range c.classes {
		if !e.classes[class] {
			return false
		}
	}
	return true
}

// Dynamic programming keeps matching linear in nodes × selector compounds.
// Ancestor and preceding-sibling reachability is carried forward once per node;
// deeply nested descendants never trigger recursive backtracking.
func (s *bodySanitizer) matchSelector(selector cssSelector) []bool {
	var previous []bool
	for part, compound := range selector.compounds {
		s.css.matchWork += len(s.elements) * (1 + len(compound.ids) + len(compound.classes))
		if s.css.matchWork > maxCSSMatchWork {
			s.oversized = true
			return nil
		}
		current := make([]bool, len(s.elements))
		reachable := make([]bool, len(s.elements))
		for i, e := range s.elements {
			if err := s.ctx.Err(); err != nil {
				s.err = err
				return nil
			}
			connected := part == 0
			if part > 0 {
				switch selector.combinators[part-1] {
				case ">":
					connected = e.parent >= 0 && previous[e.parent]
				case "+":
					connected = e.previous >= 0 && previous[e.previous]
				case " ":
					connected = e.parent >= 0 && (previous[e.parent] || reachable[e.parent])
				case "~":
					connected = e.previous >= 0 && (previous[e.previous] || reachable[e.previous])
				}
			}
			reachable[i] = connected
			current[i] = connected && compound.matches(e)
		}
		previous = current
	}
	return previous
}
