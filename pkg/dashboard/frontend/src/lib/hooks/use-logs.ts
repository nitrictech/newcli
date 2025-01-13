import { useCallback } from 'react'
import useSWR from 'swr'
import { fetcher } from './fetcher'
import type { LogEntry } from '@/types'
import { LOGS_API } from '../constants'

export const useLogs = (origin?: string) => {
  const { data, mutate } = useSWR<LogEntry[]>(
    `${LOGS_API}?origin=${origin}`,
    fetcher(),
    {
      refreshInterval: 250,
    },
  )

  const purgeLogs = useCallback(async () => {
    await fetch(LOGS_API, {
      method: 'DELETE',
    })

    return mutate()
  }, [])

  return {
    data: data || [],
    mutate,
    purgeLogs,
    loading: !data,
  }
}
