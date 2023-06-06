import type { ApiHistoryItem, Endpoint } from "../../types";
import Badge from "../shared/Badge";
import { getDateString } from "../../lib/utils";
import { Disclosure } from "@headlessui/react";
import { ChevronUpIcon } from "@heroicons/react/20/solid";
import { useMemo, useState } from "react";
import { Tabs } from "../shared";
import APIResponseContent from "./APIResponseContent";
import TableGroup from "../shared/TableGroup";
import APIRequestContent from "./ApiRequestContent";

interface Props {
  history: ApiHistoryItem[];
  selectedRequest: {
    method: string;
    path: string;
  };
}

const checkEquivalentPaths = (matcher: string, path: string): boolean => {
  // If the paths are equal regardless of query params
  if (path.split("?").length > 1 && matcher.split("?").length > 1) {
    return path.split("?")[0] === matcher.split("?")[0];
  }

  const regex = matcher.replace(/{(.*)}/, "(.*)");
  return path.match(regex) !== null;
};

const APIHistory: React.FC<Props> = ({ history, selectedRequest }) => {
  const requestHistory = history
    .sort((a, b) => b.time - a.time)
    .filter((h) => h.request && h.response)
    .filter((h) =>
      checkEquivalentPaths(selectedRequest.path ?? "", h.request.path ?? "")
    )
    .filter((h) => h.request.method === selectedRequest.method);

  if (!requestHistory.length) {
    return <p>There is no history.</p>;
  }

  return (
    <div className="flex flex-col gap-2 overflow-y-scroll max-h-[40rem]">
      {requestHistory.map((h, idx) => (
        <ApiHistoryAccordion key={idx} {...h} />
      ))}
    </div>
  );
};

const ApiHistoryAccordion: React.FC<ApiHistoryItem> = ({
  api,
  success,
  time,
  request,
  response,
}) => {
  const [tabIndex, setTabIndex] = useState(0);
  const [historyTabs, setHistoryTabs] = useState<{ name: string }[]>([]);

  useMemo(() => {
    const tabs = [{ name: "Headers" }];

    if (
      response.data &&
      Object.keys(response.headers ?? []).some(
        (header) => header.toLowerCase() === "content-type"
      )
    ) {
      tabs.push({ name: "Response" });

      if (request.body) {
        tabs.push({ name: "Payload" });
      }
    }

    setHistoryTabs(tabs);
  }, []);

  return (
    <Disclosure>
      {({ open }) => (
        <>
          <Disclosure.Button className="flex w-full justify-between rounded-lg bg-white border border-slate-100 px-4 py-2 text-left text-sm font-medium text-black hover:bg-blue-100 focus:outline-none focus-visible:ring focus-visible:ring-blue-500 focus-visible:ring-opacity-75">
            <div className="flex flex-row justify-between w-full">
              <div className="flex flex-row gap-4">
                {response.status && (
                  <Badge
                    status={success ? "green" : "red"}
                    className="!text-md w-12 sm:w-20 h-6"
                  >
                    <span className="hidden md:inline">Status: </span>
                    {response.status}
                  </Badge>
                )}
                <p className="truncate max-w-[200px] md:max-w-lg">
                  {api}
                  {request.path}
                </p>
              </div>
              <div className="flex flex-row gap-2 md:gap-4">
                <p className="hidden sm:inline">{getDateString(time)}</p>
                <ChevronUpIcon
                  className={`${
                    open ? "rotate-180 transform" : ""
                  } h-5 w-5 text-blue-500`}
                />
              </div>
            </div>
          </Disclosure.Button>
          <Disclosure.Panel className="pb-2 text-sm text-gray-500">
            <div className="flex flex-col py-4">
              <div className="bg-white shadow sm:rounded-lg">
                <Tabs
                  tabs={historyTabs}
                  index={tabIndex}
                  setIndex={setTabIndex}
                />
                <div className="py-5">
                  {tabIndex === 0 && (
                    <TableGroup
                      headers={["Key", "Value"]}
                      rowDataClassName="max-w-[100px]"
                      groups={[
                        {
                          name: "Request Headers",
                          rows: Object.entries(request.headers)
                            .filter(([key, value]) => key && value)
                            .map(([key, value]) => [
                              key.toLowerCase(),
                              value.join(", "),
                            ]),
                        },
                        {
                          name: "Response Headers",
                          rows: Object.entries(response.headers ?? [])
                            .filter(([key, value]) => key && value)
                            .map(([key, value]) => [key.toLowerCase(), value]),
                        },
                      ]}
                    />
                  )}
                  {tabIndex === 1 && (
                    <div className="flex flex-col gap-8">
                      <div className="flex flex-col gap-2">
                        <p className="text-md font-semibold">Response Data</p>
                        <APIResponseContent
                          response={{ ...response, data: atob(response.data) }}
                        />
                      </div>
                    </div>
                  )}
                  {tabIndex === 2 && (
                    <div className="flex flex-col gap-8">
                      <div className="flex flex-col gap-2">
                        <p className="text-md font-semibold">Request Body</p>
                        <APIRequestContent
                          headers={request.headers}
                          data={atob(request.body?.toString() ?? "")}
                        />
                      </div>
                      <div className="flex flex-col gap-2">
                        {request.queryParams && (
                          <TableGroup
                            headers={["Key", "Value"]}
                            rowDataClassName="max-w-[100px]"
                            groups={[
                              {
                                name: "Query Params",
                                rows: request.queryParams
                                  .filter(({ key, value }) => key && value)
                                  .map(({ key, value }) => [key, value]),
                              },
                            ]}
                          />
                        )}
                      </div>
                    </div>
                  )}
                </div>
              </div>
            </div>
          </Disclosure.Panel>
        </>
      )}
    </Disclosure>
  );
};

export default APIHistory;
