package genevieve

const AgentChooseToolSchema = `{
  "type": "object",
  "properties": {
    "input": {
      "type": "string"
    },
    "tool": {
      "type": "string"
    }
  },
  "required": [
    "input",
    "tool"
  ]
}`
