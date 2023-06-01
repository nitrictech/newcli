import { useMemo, useState } from "react";
import { useWebSocket } from "../../lib/hooks/use-web-socket";
import AppLayout from "../layout/AppLayout";
import ArchitectureDiagram from "./ArchitectureDiagram";
import { Resource, convertStackDataToResources } from "./utils";
import { CubeIcon } from "@heroicons/react/24/outline";
import ResourceTreeView from "./ResourceTreeView";

const ResourceExplorer = () => {
  const { data, loading } = useWebSocket();

  const resourceData = useMemo(() => {
    if (!data) return [] as Resource[];

    return [
      {
        type: "project",
        name: data.projectName,
        icon: <CubeIcon className="w-6" />,
      } as Resource,
      ...convertStackDataToResources({
        apis: data.apis,
        buckets: data.buckets,
        topics: data.topics,
        schedules: data.schedules,
        queues: data.queues,
        secrets: data.secrets,
        collections: data.collections,
      }),
    ];
  }, [data]);

  const defaultResource = { type: "api", name: "", icon: <></> } as Resource;

  const [selectedResource, setSelectedResource] =
    useState<Resource>(defaultResource);

  return (
    <AppLayout
      title="Resources"
      routePath={"/resources"}
      secondLevelNav={
        <>
          <span className="text-lg mb-2 px-2">Resources</span>
          <ResourceTreeView
            initialItem={selectedResource}
            onSelect={(resource) => {
              setSelectedResource(resource);
            }}
            resources={resourceData.filter((r) => r.type !== "project")}
          />
        </>
      }
    >
      {!loading && data && (
        <div className="h-[700px] w-full">
          <ArchitectureDiagram
            projectName={data.projectName}
            resources={resourceData}
            selectedResource={selectedResource}
            setSelectedResource={setSelectedResource}
          />
        </div>
      )}
    </AppLayout>
  );
};

export default ResourceExplorer;
