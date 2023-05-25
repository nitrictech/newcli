import { Fragment, PropsWithChildren, ReactNode, useState } from "react";
import { Dialog, Popover, Transition } from "@headlessui/react";
import {
  DocumentDuplicateIcon,
  Bars3Icon,
  GlobeAltIcon,
  XMarkIcon,
  ClockIcon,
  CircleStackIcon,
  MegaphoneIcon,
  QuestionMarkCircleIcon,
  PaperAirplaneIcon,
  ChatBubbleBottomCenterIcon,
  Square3Stack3DIcon,
} from "@heroicons/react/24/outline";
import classNames from "classnames";
import { useWebSocket } from "../../lib/hooks/use-web-socket";
import { Toaster } from "react-hot-toast";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "../shared/Tooltip";

const DiscordLogo: React.FC<React.SVGProps<SVGSVGElement>> = ({
  className,
}) => (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    className={className}
    viewBox="0 0 127.14 96.36"
  >
    <g id="layers" data-name="layers">
      <g id="Discord_Logos" data-name="Discord Logos">
        <g
          id="Discord_Logo_-_Large_-_White"
          data-name="Discord Logo - Large - White"
        >
          <path
            fill="#5865f2"
            d="M107.7,8.07A105.15,105.15,0,0,0,81.47,0a72.06,72.06,0,0,0-3.36,6.83A97.68,97.68,0,0,0,49,6.83,72.37,72.37,0,0,0,45.64,0,105.89,105.89,0,0,0,19.39,8.09C2.79,32.65-1.71,56.6.54,80.21h0A105.73,105.73,0,0,0,32.71,96.36,77.7,77.7,0,0,0,39.6,85.25a68.42,68.42,0,0,1-10.85-5.18c.91-.66,1.8-1.34,2.66-2a75.57,75.57,0,0,0,64.32,0c.87.71,1.76,1.39,2.66,2a68.68,68.68,0,0,1-10.87,5.19,77,77,0,0,0,6.89,11.1A105.25,105.25,0,0,0,126.6,80.22h0C129.24,52.84,122.09,29.11,107.7,8.07ZM42.45,65.69C36.18,65.69,31,60,31,53s5-12.74,11.43-12.74S54,46,53.89,53,48.84,65.69,42.45,65.69Zm42.24,0C78.41,65.69,73.25,60,73.25,53s5-12.74,11.44-12.74S96.23,46,96.12,53,91.08,65.69,84.69,65.69Z"
          />
        </g>
      </g>
    </g>
  </svg>
);

const resourceLinks = [
  {
    name: "Nitric Docs",
    href: "https://nitric.io/docs",
    icon: DocumentDuplicateIcon,
    description:
      "Unlock the power of knowledge! Dive into our docs for helpful tips, tricks, and all the information you need to make the most out of Nitric.",
  },
  {
    name: "Send Feedback",
    href: "https://github.com/nitrictech/nitric/discussions/new?category=general&title=Local%20Dashboard%20Feedback",
    icon: PaperAirplaneIcon,
    description:
      "Help us improve! Your feedback is valuable in shaping our roadmap",
  },
];

const communityLinks = [
  {
    name: "Join us on Discord",
    href: "https://discord.gg/Webemece5C",
    icon: DiscordLogo,
  },
  {
    name: "GitHub Discussions",
    href: "https://github.com/nitrictech/nitric/discussions",
    icon: ChatBubbleBottomCenterIcon,
  },
];

interface Props extends PropsWithChildren {
  title: string;
  routePath: string;
  secondLevelNav: ReactNode;
}

const AppLayout: React.FC<Props> = ({
  title = "Dev Dashboard",
  children,
  secondLevelNav,
  routePath = "/",
}) => {
  const { data } = useWebSocket();
  const [sidebarOpen, setSidebarOpen] = useState(false);

  // remove trailing slash
  routePath = routePath !== "/" ? routePath.replace(/\/$/, "") : routePath;

  const counts = {
    apis: data?.apis?.length || 0,
    schedules: data?.schedules?.length || 0,
    storage: data?.buckets?.length || 0,
    topics: data?.topics?.length || 0,
  };

  const navigation = [
    {
      name: "API Explorer",
      href: "/",
      icon: GlobeAltIcon,
      count: counts["apis"],
    },
    {
      name: "Schedules",
      href: "/schedules",
      icon: ClockIcon,
      count: counts["schedules"],
    },
    {
      name: "Storage",
      href: "/storage",
      icon: CircleStackIcon,
      count: counts["storage"],
    },
    {
      name: "Topics",
      href: "/topics",
      icon: MegaphoneIcon,
      count: counts["topics"],
    },
    {
      name: "Resources",
      href: "/resources",
      icon: Square3Stack3DIcon,
      count: Object.values(counts).reduce((prev, curr) => prev + curr, 0),
    },
    // { name: "Collections", href: "#", icon: FolderIcon, current: false },
    // { name: "Secrets", href: "#", icon: LockClosedIcon, current: false },
  ];

  return (
    <>
      <TooltipProvider>
        <Toaster position="top-right" />
        <Transition.Root show={sidebarOpen} as={Fragment}>
          <Dialog
            as="div"
            className="relative z-50 lg:hidden"
            onClose={setSidebarOpen}
          >
            <Transition.Child
              as={Fragment}
              enter="transition-opacity ease-linear duration-300"
              enterFrom="opacity-0"
              enterTo="opacity-100"
              leave="transition-opacity ease-linear duration-300"
              leaveFrom="opacity-100"
              leaveTo="opacity-0"
            >
              <div className="fixed inset-0 bg-white/80" />
            </Transition.Child>

            <div className="fixed inset-0 flex">
              <Transition.Child
                as={Fragment}
                enter="transition ease-in-out duration-300 transform"
                enterFrom="-translate-x-full"
                enterTo="translate-x-0"
                leave="transition ease-in-out duration-300 transform"
                leaveFrom="translate-x-0"
                leaveTo="-translate-x-full"
              >
                <Dialog.Panel className="relative mr-16 flex w-full max-w-xs flex-1">
                  <Transition.Child
                    as={Fragment}
                    enter="ease-in-out duration-300"
                    enterFrom="opacity-0"
                    enterTo="opacity-100"
                    leave="ease-in-out duration-300"
                    leaveFrom="opacity-100"
                    leaveTo="opacity-0"
                  >
                    <div className="absolute top-0 left-full flex w-16 justify-center pt-5">
                      <button
                        type="button"
                        className="-m-2.5 p-2.5"
                        onClick={() => setSidebarOpen(false)}
                      >
                        <span className="sr-only">Close sidebar</span>
                        <XMarkIcon
                          className="h-6 w-6 text-white"
                          aria-hidden="true"
                        />
                      </button>
                    </div>
                  </Transition.Child>
                  {/* Sidebar component, swap this element with another sidebar if you like */}
                  <div className="flex grow flex-col gap-y-5 overflow-y-auto bg-white px-6 pb-2">
                    <div className="flex h-16 shrink-0 items-center">
                      <img
                        className="h-8 w-auto"
                        src="/nitric-no-text.svg"
                        alt="Nitric Logo"
                      />
                    </div>
                    <nav className="flex flex-1 flex-col">
                      <ul className="flex flex-1 flex-col gap-y-7">
                        <li>
                          <ul className="-mx-2 space-y-1">
                            {navigation.map((item) => (
                              <li key={item.name}>
                                <a
                                  href={item.href}
                                  className={classNames(
                                    item.href === routePath
                                      ? "bg-gray-50 text-blue-600"
                                      : "text-gray-700 hover:text-blue-600 hover:bg-gray-50",
                                    "group flex gap-x-3 rounded-md p-2 items-center text-sm leading-6 font-semibold"
                                  )}
                                >
                                  <item.icon
                                    className={classNames(
                                      item.href === routePath
                                        ? "text-blue-600"
                                        : "text-gray-400 group-hover:text-blue-600",
                                      "h-6 w-6 shrink-0"
                                    )}
                                    aria-hidden="true"
                                  />
                                  <span>{item.name}</span>
                                  {item.count ? (
                                    <span
                                      data-testid={`${item.name}-count`}
                                      className="flex h-4 w-4 text-xs items-center justify-center rounded-full bg-white ring-2 ring-gray-100"
                                    >
                                      {item.count}
                                    </span>
                                  ) : null}
                                </a>
                              </li>
                            ))}
                          </ul>
                        </li>
                        <li>
                          <div className="text-xs font-semibold leading-6 text-gray-400">
                            Resources & Feedback
                          </div>
                          <ul className="-mx-2 mt-2 space-y-1">
                            {[...resourceLinks, ...communityLinks].map(
                              (link) => (
                                <li key={link.name}>
                                  <a
                                    href={link.href}
                                    target="_blank"
                                    rel="noreferrer"
                                    className={classNames(
                                      "text-gray-700 hover:text-blue-600 items-center hover:bg-gray-50",
                                      "group flex gap-x-3 rounded-md p-2 text-sm leading-6 font-semibold"
                                    )}
                                  >
                                    <span className="truncate">
                                      {link.name}
                                    </span>
                                    <link.icon className="w-4 h-4" />
                                  </a>
                                </li>
                              )
                            )}
                          </ul>
                        </li>
                      </ul>
                    </nav>
                  </div>
                </Dialog.Panel>
              </Transition.Child>
            </div>
          </Dialog>
        </Transition.Root>

        <div className="hidden lg:fixed border-r lg:inset-y-0 lg:left-0 lg:z-50 lg:block lg:w-20 lg:overflow-y-auto lg:bg-white lg:pb-4">
          <div className="flex h-16 shrink-0 items-center justify-center">
            <img
              className="h-8 w-auto"
              src="/nitric-no-text.svg"
              alt="Nitric Logo"
            />
          </div>
          <nav className="mt-6">
            <ul className="flex flex-col items-center space-y-1">
              {navigation.map((item) => (
                <li key={item.name}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <a
                        href={item.href}
                        className={classNames(
                          item.href === routePath
                            ? "bg-gray-100 text-blue-600"
                            : "text-gray-400 hover:text-blue-600 hover:bg-gray-100",
                          "group relative flex gap-x-3 rounded-md p-3 text-sm leading-6 font-semibold"
                        )}
                      >
                        <item.icon
                          className="h-6 w-6 shrink-0"
                          aria-hidden="true"
                        />
                        <span className="sr-only">{item.name}</span>
                        {item.count ? (
                          <span
                            data-testid={`${item.name}-count`}
                            className="absolute right-0 bottom-0 flex items-center justify-center h-4 w-4 text-xs -translate-y-1/2 translate-x-1/2 transform rounded-full bg-white ring-2 ring-gray-100"
                          >
                            {item.count}
                          </span>
                        ) : null}
                      </a>
                    </TooltipTrigger>
                    <TooltipContent side="right">
                      <p>{item.name}</p>
                    </TooltipContent>
                  </Tooltip>
                </li>
              ))}
            </ul>
          </nav>
        </div>

        <aside className="fixed inset-y-0 left-20 pt-20 hidden w-80 overflow-y-auto overflow-x-hidden border-r border-gray-200 py-6 xl:block">
          {secondLevelNav}
        </aside>

        <main className="lg:pl-20">
          <div className="sticky top-0 z-40 flex h-16 shrink-0 items-center gap-x-4 border-b border-gray-200 bg-white px-4 sm:gap-x-6 sm:px-6 lg:px-8">
            <button
              type="button"
              className="-m-2.5 p-2.5 text-gray-700 lg:hidden"
              onClick={() => setSidebarOpen(true)}
            >
              <span className="sr-only">Open sidebar</span>
              <Bars3Icon className="h-6 w-6" aria-hidden="true" />
            </button>
            {/* Separator */}
            <div
              className="h-6 w-px bg-gray-900/10 lg:hidden"
              aria-hidden="true"
            />
            <div className="flex gap-2 items-center md:text-lg font-semibold leading-6 text-blue-800">
              Nitric Dashboard <span className="text-gray-300">/</span> {title}
            </div>

            <div className="flex flex-1 gap-x-4 self-stretch lg:gap-x-6">
              <div className="flex ml-auto items-center gap-x-4 lg:gap-x-6">
                <Popover className="relative">
                  {({ open }) => (
                    <>
                      <Popover.Button
                        className={classNames(
                          "rounded-md bg-white flex px-2.5 py-1.5 text-sm font-semibold text-gray-800 shadow-sm ring-1 ring-inset ring-gray-300 hover:bg-gray-50",
                          open && "text-opacity-90"
                        )}
                      >
                        <span>Help</span>
                        <QuestionMarkCircleIcon
                          className={classNames(
                            "ml-2 h-5 w-5 text-blue-300 transition duration-150 ease-in-out group-hover:text-opacity-80",
                            open && "text-opacity-70"
                          )}
                          aria-hidden="true"
                        />
                      </Popover.Button>
                      <Transition
                        as={Fragment}
                        enter="transition ease-out duration-200"
                        enterFrom="opacity-0 translate-y-1"
                        enterTo="opacity-100 translate-y-0"
                        leave="transition ease-in duration-150"
                        leaveFrom="opacity-100 translate-y-0"
                        leaveTo="opacity-0 translate-y-1"
                      >
                        <Popover.Panel className="absolute left-1/2 z-10 mt-3 w-screen max-w-sm -translate-x-1/2 transform px-4 sm:px-0 lg:max-w-3xl">
                          <div className="w-screen max-w-md flex-auto overflow-hidden rounded-3xl bg-white text-sm leading-6 shadow-lg ring-1 ring-gray-900/5">
                            <div className="p-4">
                              <h3 className="text-sm font mb-2 text-center font-semibold leading-6 text-gray-500">
                                Need help with your project?
                              </h3>
                              {resourceLinks.map((item) => (
                                <div
                                  key={item.name}
                                  className="group relative flex gap-x-6 rounded-lg p-4 hover:bg-gray-50"
                                >
                                  <div className="mt-1 flex h-11 w-11 flex-none items-center justify-center rounded-lg bg-gray-50 group-hover:bg-white">
                                    <item.icon
                                      className="h-6 w-6 text-gray-600 group-hover:text-blue-600"
                                      aria-hidden="true"
                                    />
                                  </div>
                                  <div>
                                    <a
                                      href={item.href}
                                      target="_blank"
                                      rel="noreferrer"
                                      className="font-semibold text-gray-900"
                                    >
                                      {item.name}
                                      <span className="absolute inset-0" />
                                    </a>
                                    <p className="mt-1 text-gray-600">
                                      {item.description}
                                    </p>
                                  </div>
                                </div>
                              ))}
                            </div>

                            <div className="bg-gray-50">
                              <div className="flex flex-col justify-between">
                                <h3 className="text-sm font p-4 text-center font-semibold leading-6 text-gray-500">
                                  Reach out to the community
                                </h3>
                                <div className="grid grid-cols-2 divide-x divide-gray-900/5">
                                  {communityLinks.map((item) => (
                                    <a
                                      key={item.name}
                                      href={item.href}
                                      target="_blank"
                                      rel="noreferrer"
                                      className="flex items-center justify-center gap-x-2.5 p-3 font-semibold text-gray-900 hover:bg-gray-100"
                                    >
                                      <item.icon
                                        className="h-5 w-5 flex-none text-gray-400"
                                        aria-hidden="true"
                                      />
                                      {item.name}
                                    </a>
                                  ))}
                                </div>
                              </div>
                            </div>
                          </div>
                        </Popover.Panel>
                      </Transition>
                    </>
                  )}
                </Popover>
              </div>
            </div>
          </div>

          <div className="xl:pl-80">
            <div className="px-4 py-10 sm:px-6 lg:px-8 lg:py-12">
              <h1 className="text-4xl text-blue-800 font-bold mb-12">
                {title}
              </h1>
              {children}
            </div>
          </div>
        </main>
      </TooltipProvider>
    </>
  );
};

export default AppLayout;
