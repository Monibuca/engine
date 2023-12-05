package config

type Property struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Enum        []struct {
		Label string `json:"label"`
		Value any `json:"value"`
	} `json:"enum,omitempty"`
	Items          *Object        `json:"items,omitempty"`
	Properties     map[string]any `json:"properties,omitempty"`
	Default        any            `json:"default,omitempty"`
	Decorator      string         `json:"x-decorator"`
	DecoratorProps map[string]any `json:"x-decorator-props,omitempty"`
	Component      string         `json:"x-component"`
	ComponentProps map[string]any `json:"x-component-props,omitempty"`
	Index          int            `json:"x-index"`
}

type Card struct {
	Type           string         `json:"type"`
	Properties     map[string]any `json:"properties,omitempty"`
	Component      string         `json:"x-component"`
	ComponentProps map[string]any `json:"x-component-props,omitempty"`
	Index          int            `json:"x-index"`
}

type Object struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
}

type Formily struct {
	Form struct {
		LabelCol   int `json:"labelCol"`
		WrapperCol int `json:"wrapperCol"`
	} `json:"form"`
	Schema Object `json:"schema"`
}
