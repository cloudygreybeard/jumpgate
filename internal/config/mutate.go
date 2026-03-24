package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadRaw reads and parses the config file, returning both the parsed
// Config and the raw yaml.Node tree for comment-preserving mutations.
func LoadRaw(path string) (*Config, *yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = make(map[string]Context)
	}
	if cfg.DefaultContext == "" {
		cfg.DefaultContext = "default"
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parsing config tree: %w", err)
	}

	return &cfg, &doc, nil
}

// SaveRaw writes a yaml.Node tree back to disk, preserving comments.
func SaveRaw(path string, doc *yaml.Node) error {
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// SetDefaultContext updates the default_context field in the YAML tree.
func SetDefaultContext(doc *yaml.Node, name string) error {
	root := docRoot(doc)
	if root == nil {
		return fmt.Errorf("empty config document")
	}

	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "default_context" {
			root.Content[i+1].Value = name
			return nil
		}
	}

	// Key not found -- append it
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "default_context"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: name},
	)
	return nil
}

// AddContext adds a new context entry to the YAML tree.
func AddContext(doc *yaml.Node, name string, ctx Context) error {
	root := docRoot(doc)
	if root == nil {
		return fmt.Errorf("empty config document")
	}

	contextsNode := findMapValue(root, "contexts")
	if contextsNode == nil {
		// Create contexts map
		contextsKey := &yaml.Node{Kind: yaml.ScalarNode, Value: "contexts"}
		contextsNode = &yaml.Node{Kind: yaml.MappingNode}
		root.Content = append(root.Content, contextsKey, contextsNode)
	}

	// Check for duplicate
	if findMapValue(contextsNode, name) != nil {
		return fmt.Errorf("context %q already exists", name)
	}

	ctxData, err := yaml.Marshal(ctx)
	if err != nil {
		return fmt.Errorf("marshalling context: %w", err)
	}
	var ctxNode yaml.Node
	if err := yaml.Unmarshal(ctxData, &ctxNode); err != nil {
		return fmt.Errorf("parsing context node: %w", err)
	}

	contextsNode.Content = append(contextsNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: name},
		ctxNode.Content[0],
	)
	return nil
}

// SetContext creates or overwrites a context in the YAML tree.
func SetContext(doc *yaml.Node, name string, ctx Context) error {
	root := docRoot(doc)
	if root == nil {
		return fmt.Errorf("empty config document")
	}

	contextsNode := findMapValue(root, "contexts")
	if contextsNode == nil {
		contextsKey := &yaml.Node{Kind: yaml.ScalarNode, Value: "contexts"}
		contextsNode = &yaml.Node{Kind: yaml.MappingNode}
		root.Content = append(root.Content, contextsKey, contextsNode)
	}

	ctxData, err := yaml.Marshal(ctx)
	if err != nil {
		return fmt.Errorf("marshalling context: %w", err)
	}
	var ctxNode yaml.Node
	if err := yaml.Unmarshal(ctxData, &ctxNode); err != nil {
		return fmt.Errorf("parsing context node: %w", err)
	}

	for i := 0; i < len(contextsNode.Content)-1; i += 2 {
		if contextsNode.Content[i].Value == name {
			contextsNode.Content[i+1] = ctxNode.Content[0]
			return nil
		}
	}

	contextsNode.Content = append(contextsNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: name},
		ctxNode.Content[0],
	)
	return nil
}

// DeleteContext removes a context from the YAML tree.
func DeleteContext(doc *yaml.Node, name string) error {
	root := docRoot(doc)
	if root == nil {
		return fmt.Errorf("empty config document")
	}

	contextsNode := findMapValue(root, "contexts")
	if contextsNode == nil {
		return fmt.Errorf("no contexts defined")
	}

	for i := 0; i < len(contextsNode.Content)-1; i += 2 {
		if contextsNode.Content[i].Value == name {
			contextsNode.Content = append(contextsNode.Content[:i], contextsNode.Content[i+2:]...)
			return nil
		}
	}
	return fmt.Errorf("context %q not found", name)
}

// RenameContext renames a context in the YAML tree (updates the key and
// default_context if it was the default).
func RenameContext(doc *yaml.Node, oldName, newName string) error {
	root := docRoot(doc)
	if root == nil {
		return fmt.Errorf("empty config document")
	}

	contextsNode := findMapValue(root, "contexts")
	if contextsNode == nil {
		return fmt.Errorf("no contexts defined")
	}

	if findMapValue(contextsNode, newName) != nil {
		return fmt.Errorf("context %q already exists", newName)
	}

	found := false
	for i := 0; i < len(contextsNode.Content)-1; i += 2 {
		if contextsNode.Content[i].Value == oldName {
			contextsNode.Content[i].Value = newName
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("context %q not found", oldName)
	}

	// Update default_context if it was pointing to the old name
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "default_context" && root.Content[i+1].Value == oldName {
			root.Content[i+1].Value = newName
			break
		}
	}

	return nil
}

func docRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	if doc.Kind == yaml.MappingNode {
		return doc
	}
	return nil
}

func findMapValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
