import { useMemo, useState } from "react";
import { useWebSocket } from "../../lib/hooks/use-web-socket";
import AppLayout from "../layout/AppLayout";
import ArchitectureDiagram from "./ArchitectureDiagram";
import { Resource, convertStackDataToResources } from "./utils";
import ResourceTreeView from "./ResourceTreeView";

const ResourceExplorer = () => {
  const { data, loading } = useWebSocket();

  const resourceData = useMemo(
    () =>
      convertStackDataToResources({
        apis: data?.apis || [],
        buckets: data?.buckets || [],
        topics: data?.topics || [],
        schedules: data?.schedules || [],
      }),
    [data]
  );

  const defaultResource = { type: "apis", name: "", icon: <></> } as Resource;

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
            resources={resourceData}
          />
        </>
      }
    >
      <ArchitectureDiagram loading={loading} data={resourceData} />
    </AppLayout>
  );
};

export default ResourceExplorer;
