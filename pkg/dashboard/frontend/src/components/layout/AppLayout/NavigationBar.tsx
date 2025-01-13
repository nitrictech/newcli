import { cn } from '@/lib/utils'
import React, { useState } from 'react'
import type { NavigationItemProps } from './NavigationItem'
import NavigationItem from './NavigationItem'
import { Separator } from '@/components/ui/separator'
import { ListBulletIcon, HeartIcon, MapIcon } from '@heroicons/react/24/outline'

interface NavigationBarProps {
  navigation: Omit<NavigationItemProps, 'routePath'>[]
  routePath: string
}

const NavigationBar: React.FC<NavigationBarProps> = ({
  navigation,
  routePath,
}) => {
  const [navigationOpen, setNavigationOpen] = useState(false)

  const closeNavigation = () => {
    setNavigationOpen(false)
  }

  return (
    <nav
      data-state={navigationOpen ? 'expanded' : 'collapsed'}
      onMouseEnter={() => setNavigationOpen(true)}
      onMouseLeave={closeNavigation}
      className={cn(
        'group hidden flex-col overflow-hidden border-r lg:fixed lg:inset-y-0 lg:left-0 lg:z-50 lg:flex lg:w-20 lg:overflow-y-auto lg:bg-white lg:pb-4 lg:data-[state=expanded]:w-52',
        'transition-[width] duration-200 data-[state=expanded]:shadow-lg',
      )}
    >
      <div className="relative flex h-16 w-20 shrink-0 items-center justify-center">
        <img
          className="absolute h-8 w-auto"
          src="/nitric-no-text.svg"
          alt="Nitric Logo"
        />
      </div>
      <div className="mt-4 space-y-2">
        <ul className="flex flex-col justify-start gap-y-1 px-4">
          <NavigationItem
            routePath={routePath}
            icon={MapIcon}
            name="Architecture"
            href="/architecture"
            onClick={closeNavigation}
          />
          <NavigationItem
            routePath={routePath}
            icon={ListBulletIcon}
            name="Logs"
            href="/logs"
            onClick={closeNavigation}
          />
        </ul>
        <Separator />
        <ul className="flex flex-col justify-start gap-y-1 px-4">
          {navigation.map((item) => (
            <NavigationItem
              key={item.name}
              routePath={routePath}
              onClick={closeNavigation}
              {...item}
            />
          ))}
        </ul>
        <Separator />
        <ul className="flex flex-col justify-start gap-y-1 px-4">
          <NavigationItem
            routePath={routePath}
            icon={HeartIcon}
            name="Sponsor"
            href="https://github.com/sponsors/nitrictech"
          />
        </ul>
      </div>
    </nav>
  )
}

export default NavigationBar
