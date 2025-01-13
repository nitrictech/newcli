import React from 'react'
import AppLayout from '../layout/AppLayout'
import { cn } from '@/lib/utils'
import { Button } from '../ui/button'
import { useLogs } from '@/lib/hooks/use-logs'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'
import { format } from 'date-fns/format'
import { formatDistanceToNow } from 'date-fns/formatDistanceToNow'
import { ansiToReact } from './ansi'

const Logs: React.FC = () => {
  const { data: logs, purgeLogs } = useLogs('/api/logs')

  return (
    <AppLayout title="Logs" routePath="/logs">
      <div className="relative flex flex-col">
        <div className="flex">
          <Button
            data-testid="purge-logs-btn"
            onClick={purgeLogs}
            variant="destructive"
            className="ml-auto"
            title="Copy logs to clipboard"
            size="sm"
          >
            Purge Logs
          </Button>
        </div>
        <div className="mx-1 grid grid-cols-[200px_150px_1fr] gap-x-2 border-b pb-2 text-lg font-semibold">
          <span>Time</span>
          <span>Origin</span>
          <span>Message</span>
        </div>
        <div
          className="mt-1 flex flex-col font-mono text-sm"
          data-testid="logs"
        >
          {logs.length > 0 ? (
            logs.map(({ msg, time, origin, level }, i) => {
              const formattedLine = msg.trim()
              return (
                <div
                  key={i}
                  className={cn(
                    'mt-0.5 grid cursor-pointer grid-cols-[200px_150px_1fr] items-start gap-x-2 whitespace-pre-wrap rounded-sm px-1 py-[2px] hover:bg-gray-100 dark:hover:bg-gray-700',
                    {
                      'bg-red-100 hover:bg-red-200 dark:bg-red-800/70 dark:hover:bg-red-800/90':
                        level === 'error' &&
                        !msg.toLowerCase().includes('warning'),
                      'bg-orange-100 hover:bg-orange-200 dark:bg-orange-500/60 dark:hover:bg-orange-500/70':
                        level === 'error' &&
                        msg.toLowerCase().includes('warning'),
                    },
                  )}
                >
                  <Tooltip>
                    <TooltipTrigger className="w-[150px] truncate text-left">
                      <span className="w-[200px] truncate text-left">
                        {format(new Date(time), 'MMM dd, HH:mm:ss.SS')}
                      </span>
                    </TooltipTrigger>
                    <TooltipContent
                      className="grid grid-cols-2 gap-2 text-xs"
                      side="right"
                    >
                      <span>
                        {Intl.DateTimeFormat().resolvedOptions().timeZone}:{' '}
                      </span>
                      <span>
                        {format(new Date(time), 'MMM dd, HH:mm:ss.SS')}
                      </span>
                      <span>Relative:</span>
                      <span>
                        {formatDistanceToNow(new Date(time), {
                          addSuffix: true,
                        })}
                      </span>
                      <span>Timestamp:</span>
                      <span>{new Date(time).getTime()}</span>
                    </TooltipContent>
                  </Tooltip>

                  <Tooltip>
                    <TooltipTrigger className="w-[150px] truncate text-left">
                      <span data-testid={`test-row${i}-origin`}>{origin}</span>
                    </TooltipTrigger>
                    <TooltipContent className="text-xs">
                      <p>{origin}</p>
                    </TooltipContent>
                  </Tooltip>
                  <span
                    className="border-l pl-2 text-left"
                    data-testid={`test-row${i}-msg`}
                  >
                    {ansiToReact(formattedLine)}
                  </span>
                </div>
              )
            })
          ) : (
            <div className="mt-4 p-1 text-center font-body text-lg font-semibold tracking-wide text-gray-500 dark:text-gray-400">
              No logs available
            </div>
          )}
        </div>
      </div>
    </AppLayout>
  )
}

export default Logs
