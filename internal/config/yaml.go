package config

import (
	"bytes"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

func parseOrEmpty(raw []byte) (*yaml.Node, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return &yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode}},
		}, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode {
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{&doc}}, nil
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		doc.Content[0] = &yaml.Node{Kind: yaml.MappingNode, HeadComment: root.HeadComment}
	}
	return &doc, nil
}

func mappingOf(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode {
		return doc.Content[0]
	}
	return doc
}

func setNodeKey(doc *yaml.Node, key, value string) error {
	return setPath(mappingOf(doc), strings.Split(key, "."), value)
}

func setPath(m *yaml.Node, parts []string, value string) error {
	if m.Kind != yaml.MappingNode {
		*m = yaml.Node{Kind: yaml.MappingNode, HeadComment: m.HeadComment, LineComment: m.LineComment, FootComment: m.FootComment}
	}
	k := parts[0]
	rest := parts[1:]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != k {
			continue
		}
		val := m.Content[i+1]
		if len(rest) == 0 {
			val.Kind = yaml.ScalarNode
			val.Tag = "!!str"
			val.Value = value
			val.Content = nil
			val.Style = quoteStyle(value)
			return nil
		}
		return setPath(val, rest, value)
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
	var valNode *yaml.Node
	if len(rest) == 0 {
		valNode = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: quoteStyle(value)}
	} else {
		valNode = &yaml.Node{Kind: yaml.MappingNode}
		if err := setPath(valNode, rest, value); err != nil {
			return err
		}
	}
	m.Content = append(m.Content, keyNode, valNode)
	return nil
}

func quoteStyle(s string) yaml.Style {
	if s == "" || strings.TrimSpace(s) != s || strings.ContainsAny(s, ":#{}[],&*!|>%@`'\n") {
		return yaml.DoubleQuotedStyle
	}
	return 0
}

func encodeNode(doc *yaml.Node) ([]byte, error) {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 && doc.Content[0].HeadComment == "" && doc.HeadComment != "" {
		doc.Content[0].HeadComment = doc.HeadComment
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	var err error
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		err = enc.Encode(doc.Content[0])
	} else {
		err = enc.Encode(doc)
	}
	if cerr := enc.Close(); err == nil {
		err = cerr
	}
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out, err
}
