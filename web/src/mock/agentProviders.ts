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
      { id: 'gpt-5.6-codex', label: 'GPT-5.6 Codex', reasoning: ['Medium', 'High', 'Extra High'], supportsFast: true },
      { id: 'gpt-5.6', label: 'GPT-5.6', reasoning: ['Low', 'Medium', 'High'], supportsFast: true },
    ],
  },
  {
    id: 'Claude',
    label: 'Claude',
    connected: true,
    description: 'Anthropic coding agent',
    models: [
      { id: 'claude-sonnet-4-5', label: 'Claude Sonnet 4.5', reasoning: ['Standard', 'Extended'], supportsFast: false },
      { id: 'claude-opus-4-1', label: 'Claude Opus 4.1', reasoning: ['Standard', 'Extended'], supportsFast: false },
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
