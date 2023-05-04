import { useEffect, useMemo, useState } from "react";
import { useWebSocket } from "../../lib/use-web-socket";
import Select from "../shared/Select";
import type {
  APIRequest,
  APIResponse,
  Endpoint,
  HistoryItem,
  Method,
} from "../../types";
import Badge from "../shared/Badge";
import { fieldRowArrToHeaders, getHost } from "../../lib/utils";
import Spinner from "../shared/Spinner";
import Tabs from "../layout/Tabs";
import FieldRows, { FieldRow } from "../shared/FieldRows";
import { flattenPaths } from "./flatten-paths";
import { generatePath } from "./generate-path";
import APIResponseContent from "./APIResponseContent";
import { formatFileSize } from "./format-file-size";
import CodeEditor from "./CodeEditor";
import APIMenu from "./APIMenu";
import { generatePathParams } from "./generate-path-params";
import { generateResponse } from "../../lib/generate-response";
import { formatResponseTime } from "./format-response-time";
import Loading from "../shared/Loading";
import classNames from "classnames";

const getTabCount = (rows: FieldRow[]) => rows.filter((r) => !!r.key).length;

export const LOCAL_STORAGE_KEY = "nitric-local-dash-api-history";

const requestDefault = {
  pathParams: [],
  queryParams: [
    {
      key: "",
      value: "",
    },
  ],
  headers: [
    {
      key: "Accept",
      value: "*/*",
    },
    {
      key: "User-Agent",
      value: "Nitric Client (https://www.nitric.io)",
    },
  ],
};

const APIExplorer = () => {
  const { data, loading } = useWebSocket();
  const [callLoading, setCallLoading] = useState(false);

  const [JSONBody, setJSONBody] = useState<string>("");

  const [request, setRequest] = useState<APIRequest>(requestDefault);
  const [response, setResponse] = useState<APIResponse>();

  const [selectedApiEndpoint, setSelectedApiEndpoint] = useState<Endpoint>();
  const [currentTabIndex, setCurrentTabIndex] = useState(0);
  const [responseTabIndex, setResponseTabIndex] = useState(0);

  const [validRequest, setValidRequest] = useState(true);

  const paths = useMemo(
    () => data?.apis.map((doc) => flattenPaths(doc)).flat(),
    [data]
  );

  // Load history from localStorage on mount
  useEffect(() => {
    if (selectedApiEndpoint) {
      const storedHistory = localStorage.getItem(
        `${LOCAL_STORAGE_KEY}-${selectedApiEndpoint.id}`
      );

      if (storedHistory) {
        const history: HistoryItem = JSON.parse(storedHistory);
        setJSONBody(history.JSONBody);
        setRequest({
          ...history.request,
          pathParams: generatePathParams(selectedApiEndpoint, history.request),
        });
      } else {
        // clear
        setJSONBody("");
        setRequest({
          ...requestDefault,
          method: selectedApiEndpoint.methods[0],
          pathParams: generatePathParams(selectedApiEndpoint, requestDefault),
        });
      }

      // set history
      localStorage.setItem(
        `${LOCAL_STORAGE_KEY}-last-path-id`,
        selectedApiEndpoint.id
      );

      // clear response
      setResponse(undefined);
    }
  }, [selectedApiEndpoint]);

  useEffect(() => {
    // Validate that all path params are added
    if (request.pathParams.some((p) => !p.value)) {
      setValidRequest(false);
      return;
    }
    setValidRequest(true);
  }, [request.pathParams]);

  useEffect(() => {
    if (paths?.length) {
      // restore history or select first if not selected
      if (!selectedApiEndpoint) {
        const previousId = localStorage.getItem(
          `${LOCAL_STORAGE_KEY}-last-path-id`
        );

        const path =
          (previousId && paths.find((p) => p.id === previousId)) || paths[0];

        setSelectedApiEndpoint(path);
        setRequest((prev) => ({
          ...prev,
          method: path.methods[0],
        }));
      } else {
        // could be a refresh from ws, so update the selected endpoint
        const latest = paths.find((p) => p.id === selectedApiEndpoint.id);

        if (latest) {
          setSelectedApiEndpoint(latest);

          if (request.method && !latest.methods.includes(request.method)) {
            setRequest((prev) => ({
              ...prev,
              method: latest?.methods[0],
            }));
          }
        }
      }
    }
  }, [paths]);

  useEffect(() => {
    if (
      request.method &&
      selectedApiEndpoint &&
      !selectedApiEndpoint.methods.includes(request.method)
    ) {
      setRequest((prev) => ({
        ...prev,
        method: selectedApiEndpoint.methods[0],
      }));
    }
  }, [request.method]);

  useEffect(() => {
    if (selectedApiEndpoint) {
      const generatedPath = generatePath(
        selectedApiEndpoint,
        request.pathParams,
        request.queryParams
      );

      setRequest((prev) => ({
        ...prev,
        path: generatedPath,
      }));
    }
  }, [selectedApiEndpoint, request.pathParams, request.queryParams]);

  // Save state to local storage whenever it changes
  useEffect(() => {
    if (selectedApiEndpoint) {
      localStorage.setItem(
        `${LOCAL_STORAGE_KEY}-${selectedApiEndpoint.id}`,
        JSON.stringify({
          request,
          JSONBody,
        })
      );
    }
  }, [request, JSONBody]);

  const apiAddress = selectedApiEndpoint
    ? data?.apiAddresses[selectedApiEndpoint.api]
    : null;

  const handleSend = async (
    e: React.MouseEvent<HTMLButtonElement, MouseEvent>
  ) => {
    if (!selectedApiEndpoint) return;
    setCallLoading(true);
    e.preventDefault();

    const { path, method, headers } = request;

    const url = `http://${getHost()}/call` + path;
    const requestOptions: RequestInit = {
      method,
      headers: fieldRowArrToHeaders([
        ...headers,
        {
          key: "X-Nitric-Local-Call-Address",
          value: apiAddress || "localhost:4001",
        },
      ]),
    };

    if (method !== "GET" && method !== "HEAD" && JSONBody.trim()) {
      requestOptions.body = JSONBody;
    }
    const startTime = window.performance.now();
    const res = await fetch(url, requestOptions);

    const callResponse = await generateResponse(res, startTime);
    setResponse(callResponse);

    setTimeout(() => setCallLoading(false), 300);
  };

  const tabs = [
    {
      name: "Params",
      count: getTabCount(request.queryParams) + getTabCount(request.pathParams),
    },
    { name: "Headers", count: getTabCount(request.headers) },
    { name: "Body", count: JSONBody ? 1 : undefined },
  ];

  const currentTabName = tabs[currentTabIndex].name;

  return (
    <Loading
      delay={400}
      conditionToShow={Boolean(paths && selectedApiEndpoint && request?.method)}
    >
      {paths && selectedApiEndpoint && request?.method ? (
        <div className="flex max-w-7xl flex-col md:flex-row gap-8 md:pr-8">
          <div className="w-full md:w-7/12 flex flex-col gap-8">
            <div>
              <div className="flex">
                <h2 className="text-2xl font-medium text-blue-900">
                  API - {selectedApiEndpoint.api}
                </h2>
                <APIMenu
                  selected={selectedApiEndpoint}
                  onAfterClear={() => {
                    setJSONBody("");
                    setRequest({
                      ...requestDefault,
                      method: selectedApiEndpoint.methods[0],
                      path: generatePath(selectedApiEndpoint, [], []),
                    });
                  }}
                />
              </div>
              <nav className="flex items-end gap-4" aria-label="Breadcrumb">
                <ol className="flex w-11/12 items-center gap-4">
                  <li className="w-9/12">
                    <Select<Endpoint>
                      items={paths}
                      label="Endpoint"
                      id="endpoint-select"
                      selected={selectedApiEndpoint}
                      setSelected={setSelectedApiEndpoint}
                      display={(v) => (
                        <div className="flex items-center p-0.5 text-lg gap-4">
                          <span>{v?.api}</span>
                          <span>{v?.path}</span>
                          <span className="ml-auto px-2 text-sm">
                            {v?.methods.length} methods
                          </span>
                        </div>
                      )}
                    />
                  </li>
                  <li className="w-3/12">
                    <Select<Method>
                      items={selectedApiEndpoint?.methods || []}
                      id="method-select"
                      label="Method"
                      selected={request.method}
                      setSelected={(m) => {
                        setRequest((prev) => ({
                          ...prev,
                          method: m,
                        }));
                      }}
                      display={(method) => (
                        <Badge
                          status={
                            (
                              {
                                DELETE: "red",
                                POST: "green",
                                PUT: "yellow",
                                GET: "blue",
                              } as any
                            )[method]
                          }
                          className="!text-lg"
                        >
                          {method}
                        </Badge>
                      )}
                    />
                  </li>
                </ol>
                <div className="ml-auto">
                  <button
                    type="button"
                    data-testid="send-api-btn"
                    onClick={handleSend}
                    disabled={!validRequest}
                    className={classNames(
                      "inline-flex items-center rounded-md px-4 py-3 text-lg font-semibold text-white shadow-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2",
                      validRequest
                        ? "bg-blue-600 hover:bg-blue-500 focus-visible:outline-blue-600"
                        : "bg-gray-400 hover:bg-gray-300 focus-visible:outline-gray-400"
                    )}
                  >
                    Send
                  </button>
                </div>
              </nav>
            </div>

            <div className="bg-white shadow sm:rounded-lg">
              <Tabs
                tabs={tabs}
                index={currentTabIndex}
                setIndex={setCurrentTabIndex}
              />
              <div className="px-4 py-5 sm:p-6">
                <div className="sm:flex sm:items-start sm:justify-between">
                  <div className="w-full">
                    <div className="relative flex w-full">
                      <h3 className="text-xl font-semibold leading-6 text-gray-900">
                        {currentTabName}
                      </h3>
                      <p className="absolute text-gray-500 text-sm top-0 right-0">
                        <a
                          data-testid="generated-request-path"
                          href={`http://${apiAddress}${request.path}`}
                          target="_blank"
                          rel="noreferrer"
                        >
                          http://{apiAddress}
                          {request.path}
                        </a>
                      </p>
                    </div>
                    {currentTabName === "Params" && (
                      <ul className="divide-gray-200 my-4">
                        <li className="flex flex-col py-4">
                          <h4 className="text-lg font-medium text-gray-900">
                            Query Params
                          </h4>
                          <FieldRows
                            rows={request.queryParams}
                            testId="query"
                            setRows={(rows) => {
                              setRequest((prev) => ({
                                ...prev,
                                queryParams: rows,
                              }));
                            }}
                          />
                        </li>
                        {request.pathParams.length > 0 && (
                          <li className="flex flex-col py-4">
                            <h4 className="text-lg font-medium text-gray-900">
                              Path Params
                            </h4>
                            <FieldRows
                              lockKeys
                              testId="path"
                              required={true}
                              rows={request.pathParams}
                              setRows={(rows) => {
                                setRequest((prev) => ({
                                  ...prev,
                                  pathParams: rows,
                                }));
                              }}
                            />
                          </li>
                        )}
                      </ul>
                    )}
                    {currentTabName === "Headers" && (
                      <div className="my-4">
                        <FieldRows
                          rows={request.headers}
                          testId="header"
                          setRows={(rows) => {
                            setRequest((prev) => ({
                              ...prev,
                              headers: rows,
                            }));
                          }}
                        />
                      </div>
                    )}
                    {currentTabName === "Body" && (
                      <div className="my-4">
                        <CodeEditor
                          id="json-editor"
                          contentType={"application/json"}
                          value={JSONBody}
                          includeLinters
                          onChange={(value) => {
                            setJSONBody(value);
                          }}
                        />
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </div>
            <div className="bg-white shadow sm:rounded-lg">
              <div className="px-4 py-5 sm:p-6">
                <div className="sm:flex sm:items-start sm:justify-between">
                  <div className="w-full relative">
                    <div className="flex items-center gap-4">
                      <h3 className="text-xl font-semibold leading-6 text-gray-900">
                        Response
                      </h3>
                      {callLoading && (
                        <Spinner
                          className="absolute top-0"
                          color="info"
                          size={"md"}
                        />
                      )}
                    </div>
                    <div className="absolute right-0 top-0 flex gap-2">
                      {response?.status && (
                        <Badge
                          data-testid="response-status"
                          status={response.status >= 400 ? "red" : "green"}
                        >
                          Status: {response.status}
                        </Badge>
                      )}
                      {response?.time && (
                        <Badge data-testid="response-time" status={"green"}>
                          Time: {formatResponseTime(response.time)}
                        </Badge>
                      )}
                      {typeof response?.size === "number" && (
                        <Badge data-testid="response-size" status={"green"}>
                          Size: {formatFileSize(response.size)}
                        </Badge>
                      )}
                    </div>

                    <div className="my-4 max-w-full text-sm">
                      {response?.data ? (
                        <div className="flex flex-col gap-4">
                          <Tabs
                            tabs={[
                              {
                                name: "Response",
                              },
                              {
                                name: "Headers",
                                count: Object.keys(response.headers || {})
                                  .length,
                              },
                            ]}
                            round
                            index={responseTabIndex}
                            setIndex={setResponseTabIndex}
                          />
                          {responseTabIndex === 0 && (
                            <APIResponseContent response={response} />
                          )}
                          {responseTabIndex === 1 && (
                            <div className="overflow-x-auto">
                              <div className="inline-block min-w-full py-2 align-middle">
                                <table className="min-w-full divide-y divide-gray-300">
                                  <thead>
                                    <tr>
                                      <th
                                        scope="col"
                                        className="py-3.5 pl-4 pr-3 text-left text-sm font-semibold text-gray-900 sm:pl-6 lg:pl-8"
                                      >
                                        Header
                                      </th>
                                      <th
                                        scope="col"
                                        className="px-3 py-3.5 text-left text-sm font-semibold text-gray-900"
                                      >
                                        Value
                                      </th>
                                    </tr>
                                  </thead>
                                  <tbody className="divide-y divide-gray-200 bg-white">
                                    {Object.entries(response.headers || {}).map(
                                      ([key, value]) => (
                                        <tr key={key}>
                                          <td className="whitespace-nowrap py-4 pl-4 pr-3 text-sm font-medium text-gray-900 sm:pl-6 lg:pl-8">
                                            {key}
                                          </td>
                                          <td className="whitespace-nowrap px-3 py-4 text-sm text-gray-500">
                                            {value}
                                          </td>
                                        </tr>
                                      )
                                    )}
                                  </tbody>
                                </table>
                              </div>
                            </div>
                          )}
                        </div>
                      ) : response ? (
                        <span className="text-gray-500 text-lg">
                          No response data available for this request.
                        </span>
                      ) : (
                        <span className="text-gray-500 text-lg">
                          Send a request to get a response.
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div className="w-5/12 flex flex-col gap-12 px-8">
            <h3 className="text-2xl font-semibold opacity-70 leading-6 text-gray-900">
              History (Coming soon)
            </h3>
            {/* <APIHistory history={history} /> */}
          </div>
        </div>
      ) : null}
    </Loading>
  );
};

export default APIExplorer;
