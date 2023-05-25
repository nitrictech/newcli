import React, { useEffect, useMemo, useState } from "react";
import ReactFlow, { Node, Edge } from "react-flow-renderer";
import type { StackData } from "../../types";
import { Resource, getPositionedElements } from "./utils";

interface Props {
  data: Resource[];
  loading: boolean;
}

const StackGraphView: React.FC<Props> = ({ data, loading }) => {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [edges, setEdges] = useState<Edge[]>([]);

  useMemo(() => {
    if (!data) return;

    const nodes = data.map(
      (resource, idx) =>
        ({
          id: `${resource.type}-${idx}`,
          data: {
            label: (
              <div>
                <p>{resource.name}</p>
                {resource.icon}
              </div>
            ),
          },
          position: { x: 0, y: 0 },
        } as Node)
    );

    const edges = [] as Edge[];

    const { nodes: positionedNodes, edges: positionedEdges } =
      getPositionedElements(nodes, edges);

    setNodes([...positionedNodes]);
    setEdges([...positionedEdges]);
  }, [data]);

  return (
    <div className="h-[700px] w-full">
      {!loading && data && (
        <ReactFlow nodes={nodes} edges={edges} defaultZoom={1.5} fitView />
      )}
    </div>
  );
};
export default StackGraphView;
