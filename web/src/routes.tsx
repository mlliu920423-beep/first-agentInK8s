import { Route, Routes } from 'react-router-dom'
import ChatPage from '@/pages/ChatPage'
import ConfigAgents from '@/pages/ConfigAgents'
import ConfigMCP from '@/pages/ConfigMCP'
import ConfigSkills from '@/pages/ConfigSkills'

export default function AppRoutes() {
  return (
    <Routes>
      <Route index element={<ChatPage />} />
      <Route path="config/agents" element={<ConfigAgents />} />
      <Route path="config/mcp" element={<ConfigMCP />} />
      <Route path="config/skills" element={<ConfigSkills />} />
    </Routes>
  )
}
