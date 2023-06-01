import type { ReactNode } from "react";
import type { WorkerResource, APIDoc, StackData } from "../../../types";
import {
  GlobeAltIcon,
  ClockIcon,
  CircleStackIcon,
  MegaphoneIcon,
  LockClosedIcon,
  TableCellsIcon,
  RectangleStackIcon,
} from "@heroicons/react/24/outline";
import { CubeIcon } from "@heroicons/react/20/solid";

type NodeResource = WorkerResource | string | APIDoc;

export type ResourceType = "api" | "bucket" | "topic" | "schedule" | "project";

export interface Resource {
  type: ResourceType;
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

function getResourceIcon(type: string): JSX.Element {
  switch (type) {
    case "schedules":
      return <ClockIcon className="w-6" />;
    case "topics":
      return <MegaphoneIcon className="w-6" />;
    case "buckets":
      return <CircleStackIcon className="w-6" />;
    case "apis":
      return <GlobeAltIcon className="w-6" />;
    case "collections":
      return <TableCellsIcon className="w-6" />;
    case "secrets":
      return <LockClosedIcon className="w-6" />;
    case "queues":
      return <RectangleStackIcon className="w-6" />;
    default:
      return <CubeIcon className="w-6" />;
  }
}

export const convertStackDataToResources = (
  stackData: StackData
): Resource[] => {
  if (!stackData) return [];

  return (
    Object.entries(stackData) as [keyof StackData, NodeResource[]][]
  ).flatMap(([resourceType, resources]) => {
    if (!resources) return [];

    return resources.map(
      (resource) =>
        ({
          type: resourceType.slice(0, -1) as ResourceType,
          name: getResourceName(resource),
          icon: getResourceIcon(resourceType),
        } as Resource)
    );
  });
};
