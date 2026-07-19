import Sidebar from '@/components/Sidebar'
import AppRoutes from '@/routes'

export default function App() {
  return (
    <div className="flex h-screen bg-background text-foreground">
      <Sidebar />
      <main className="flex-1 overflow-hidden">
        <AppRoutes />
      </main>
    </div>
  )
}
