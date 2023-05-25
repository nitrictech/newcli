import dagre from "dagre";
import type { Node, Edge } from "react-flow-renderer";

const dagreGraph = new dagre.graphlib.Graph();
dagreGraph.setDefaultEdgeLabel(() => ({}));

const nodeWidth = 200;
const nodeHeight = 200;

export const getPositionedElements = (nodes: Node[], edges: Edge[]) => {
  dagreGraph.setGraph({ rankdir: "TB" });

  nodes.forEach((node) => {
    dagreGraph.setNode(node.id, { width: nodeWidth, height: nodeHeight });
  });

  edges.forEach((edge) => {
    dagreGraph.setEdge(edge.source, edge.target);
  });

  dagre.layout(dagreGraph);

  const positionedNodes = nodes.map((node) => {
    const nodeWithPosition = dagreGraph.node(node.id);

    // We are shifting the dagre node position (anchor=center center) to the top left
    // so it matches the React Flow node anchor point (top left).
    node.position = {
      x: nodeWithPosition.x - nodeWidth / 2,
      y: nodeWithPosition.y - nodeHeight / 2,
    };

    return node;
  });

  return { nodes: positionedNodes, edges };
};
