import React, { useMemo, useState } from "react";
import ReactFlow, { Node, Edge } from "reactflow";
import { Resource, getPositionedElements } from "./utils";
import ResourceNode from "./ResourceNode";

import "reactflow/dist/style.css";

interface Props {
  projectName: string;
  resources: Resource[];
  selectedResource: Resource;
  setSelectedResource: (resource: Resource) => void;
}

const ArchitectureDiagram: React.FC<Props> = ({
  resources,
  projectName,
  selectedResource,
  setSelectedResource,
}) => {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [edges, setEdges] = useState<Edge[]>([]);

  if (!resources) return null;

  useMemo(() => {
    const nodes: Node[] = resources.map((resource) => ({
      id: `${resource.type}-${resource.name}`,
      position: { x: 0, y: 0 },
      selected: resource === selectedResource,
      type: "resource",
      data: {
        resource,
        selectedResource,
        setSelectedResource: setSelectedResource.bind(null, resource),
      },
    }));

    // Connect resources to project
    const edges: Edge[] = resources
      .filter((resource) => resource.type !== "project")
      .map((resource) => ({
        id: `e-${resource.type}${resource.name}`,
        source: `project-${projectName}`,
        target: `${resource.type}-${resource.name}`,
      }));

    const { nodes: positionedNodes, edges: positionedEdges } =
      getPositionedElements(nodes, edges);

    setNodes(positionedNodes);
    setEdges(positionedEdges);
  }, [resources]);

  const nodeTypes = useMemo(() => ({ resource: ResourceNode }), []);

  return <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypes} />;
};

export default ArchitectureDiagram;
