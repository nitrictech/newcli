import classNames from "classnames";
import { Handle, Node, NodeProps, Position } from "reactflow";
import type { Resource } from "./utils";
import { memo } from "react";

type NodeData = {
  resource: Resource;
  selectedResource: Resource;
  setSelectedResource: () => void;
};

export type ResourceNode = Node<NodeData>;

export default memo(({ data }: NodeProps<NodeData>) => {
  const { resource, selectedResource, setSelectedResource } = data;

  const isSelected = resource === selectedResource;

  return (
    <>
      <Handle type="target" position={Position.Top} />
      <div
        onMouseEnter={() => setSelectedResource()}
        className={classNames(
          "flex flex-col gap-2 w-48 p-2 justify-center items-center rounded-md",
          isSelected ? "border-black border-4" : "border-gray-700 border"
        )}
      >
        {resource.icon}
        <p className="truncate">{resource.name}</p>
      </div>
      <Handle type="source" position={Position.Bottom} />
    </>
  );
});
