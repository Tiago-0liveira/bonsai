import type { AgentProvider } from '../types'

export interface AgentModelConfig {
  id: string
  label: string
  reasoning: string[]
  supportsFast: boolean
}

export interface AgentProviderConfig {
  id: AgentProvider
  label: string
  connected: boolean
  description: string
  models: AgentModelConfig[]
}

export const agentProviders: AgentProviderConfig[] = [
  {
    id: 'Codex',
    label: 'Codex',
    connected: true,
    description: 'OpenAI coding agent',
    models: [
      { id: 'gpt-6.1-sol', label: 'GPT-6.1-Sol', reasoning: ['Low', 'Medium', 'High', 'Extra High', 'Max', 'Ultra'], supportsFast: true },
      { id: 'gpt-6-luna', label: 'GPT-6-Luna', reasoning: ['Low', 'Medium', 'High', 'Extra High', 'Max'], supportsFast: true },
    ],
  },
  {
    id: 'Claude',
    label: 'Claude',
    connected: true,
    description: 'Anthropic coding agent',
    models: [
      { id: 'claude-sonnet-5-5', label: 'Claude Sonnet 5.5', reasoning: ['Low', 'Medium', 'High'], supportsFast: false },
      { id: 'claude-opus-5-5', label: 'Claude Opus 5.5', reasoning: ['Low', 'Medium', 'High'], supportsFast: false },
    ],
  },
  {
    id: 'Gemini',
    label: 'Gemini',
    connected: true,
    description: 'Google coding agent',
    models: [
      { id: 'gemini-2.5-pro', label: 'Gemini 2.5 Pro', reasoning: ['Medium', 'High'], supportsFast: false },
      { id: 'gemini-2.5-flash', label: 'Gemini 2.5 Flash', reasoning: ['Low', 'Medium'], supportsFast: true },
    ],
  },
]
