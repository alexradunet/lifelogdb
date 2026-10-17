package view

import (
	"testing"
	"text/template/parse"
)

// calledTemplates are the names of the templates that the tree n calls, at any depth.
func calledTemplates(n parse.Node, called map[string]bool) {
	switch n := n.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, c := range n.Nodes {
			calledTemplates(c, called)
		}
	case *parse.TemplateNode:
		called[n.Name] = true
	case *parse.IfNode:
		calledTemplates(n.List, called)
		calledTemplates(n.ElseList, called)
	case *parse.RangeNode:
		calledTemplates(n.List, called)
		calledTemplates(n.ElseList, called)
	case *parse.WithNode:
		calledTemplates(n.List, called)
		calledTemplates(n.ElseList, called)
	}
}

// TestTheStylePageCallsEveryComponent: the style page calls the template of each component that has one. What each
// draws is TestTheStylePageRunsTheRealTemplates's, in package api, which has the synthetic entity of the page.
func TestTheStylePageCallsEveryComponent(t *testing.T) {
	called := map[string]bool{}
	calledTemplates(templates.Lookup("styleguide").Tree.Root, called)
	for _, name := range []string{"leaf", "mood", "step", "form", "boxed", "body-editor", "onthisday", "reading-link",
		"add-reading", "metric-cards", "weekdays", "monthbox", "readings", "pager"} {
		if !called[name] {
			t.Errorf("the style page does not call the template %s", name)
		}
	}
}
