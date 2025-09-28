'use client'

import { StatsCards } from '@/components/dashboard/StatsCards'
import { StorageChart } from '@/components/dashboard/StorageChart'
import { RecentActivity } from '@/components/dashboard/RecentActivity'
import { SystemHealth } from '@/components/dashboard/SystemHealth'

export default function Dashboard() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-gray-900">Dashboard</h1>
        <p className="text-gray-600 mt-2">Overview of your MaxIOFS object storage system</p>
      </div>

      <StatsCards />

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <StorageChart />
        <SystemHealth />
      </div>

      <RecentActivity />
    </div>
  )
}