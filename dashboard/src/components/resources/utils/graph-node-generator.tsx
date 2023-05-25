import type { ReactNode } from "react";
import type { WorkerResource, APIDoc, StackData } from "../../../types";
import {
  GlobeAltIcon,
  ClockIcon,
  CircleStackIcon,
  MegaphoneIcon,
} from "@heroicons/react/24/outline";

type NodeResource = WorkerResource | string | APIDoc;

export interface Resource {
  type: keyof StackData;
  name: string;
  icon: ReactNode;
}

function isWorkerResource(resource: NodeResource): resource is WorkerResource {
  return (resource as WorkerResource).workerKey !== undefined;
}

function isAPIDoc(resource: NodeResource): resource is APIDoc {
  return (resource as APIDoc).info !== undefined;
}

function getResourceName(resource: NodeResource): string {
  if (isAPIDoc(resource)) {
    return resource.info.title;
  } else if (isWorkerResource(resource)) {
    return (resource as WorkerResource).workerKey;
  }
  return resource;
}

function getResourceIcon(type: string) {
  switch (type) {
    case "schedules":
      return <ClockIcon className="w-6" />;
    case "topics":
      return <MegaphoneIcon className="w-6" />;
    case "buckets":
      return <CircleStackIcon className="w-6" />;
    case "apis":
      return <GlobeAltIcon className="w-6" />;
  }
}

export const convertStackDataToResources = (
  stackData: StackData
): Resource[] => {
  return (
    Object.entries(stackData) as [keyof StackData, NodeResource[]][]
  ).flatMap(([resourceType, resources]) => {
    return resources.map(
      (resource) =>
        ({
          type: resourceType,
          name: getResourceName(resource),
          icon: getResourceIcon(resourceType),
        } as Resource)
    );
  });
};
