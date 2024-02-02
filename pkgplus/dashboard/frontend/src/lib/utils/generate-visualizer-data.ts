import {
  APINode,
  type ApiNodeData,
} from '@/components/visualizer/nodes/APINode'
import {
  BucketNode,
  type BucketNodeData,
} from '@/components/visualizer/nodes/BucketNode'
import type { BaseResource, Policy, WebSocketResponse } from '@/types'
import {
  ChatBubbleLeftRightIcon,
  ArchiveBoxIcon,
  ClockIcon,
  CircleStackIcon,
  CpuChipIcon,
  MegaphoneIcon,
  GlobeAltIcon,
} from '@heroicons/react/24/outline'
import { MarkerType, type Edge, type Node, Position } from 'reactflow'
import {
  TopicNode,
  type TopicNodeData,
} from '@/components/visualizer/nodes/TopicNode'
import {
  WebsocketNode,
  type WebsocketNodeData,
} from '@/components/visualizer/nodes/WebsocketNode'
import {
  KeyValueNode,
  type KeyValueNodeData,
} from '@/components/visualizer/nodes/KeyValueNode'
import {
  ScheduleNode,
  type ScheduleNodeData,
} from '@/components/visualizer/nodes/ScheduleNode'
import {
  ServiceNode,
  type ServiceNodeData,
} from '@/components/visualizer/nodes/ServiceNode'

import { OpenAPIV3 } from 'openapi-types'
import { getBucketNotifications } from './get-bucket-notifications'

export const nodeTypes = {
  api: APINode,
  bucket: BucketNode,
  schedule: ScheduleNode,
  topic: TopicNode,
  websocket: WebsocketNode,
  service: ServiceNode,
  keyvaluestore: KeyValueNode,
}

const createNode = <T>(
  resource: BaseResource,
  type: keyof typeof nodeTypes,
  data: T,
): Node<T> => {
  const nodeId = `${type}-${resource.name}`

  // Generate edges from requestingServices
  return {
    id: nodeId,
    position: { x: 0, y: 0 },
    type,
    data,
  }
}

const AllHttpMethods = [
  OpenAPIV3.HttpMethods.GET,
  OpenAPIV3.HttpMethods.PUT,
  OpenAPIV3.HttpMethods.POST,
  OpenAPIV3.HttpMethods.DELETE,
  OpenAPIV3.HttpMethods.OPTIONS,
  // OpenAPIV3.HttpMethods.HEAD,
  // OpenAPIV3.HttpMethods.PATCH,
  // OpenAPIV3.HttpMethods.TRACE,
]

// this helper function returns the intersection point
// of the line between the center of the intersectionNode and the target node
function getNodeIntersection(intersectionNode: any, targetNode: any) {
  // https://math.stackexchange.com/questions/1724792/an-algorithm-for-finding-the-intersection-point-between-a-center-of-vision-and-a
  const {
    width: intersectionNodeWidth,
    height: intersectionNodeHeight,
    positionAbsolute: intersectionNodePosition,
  } = intersectionNode;
  const targetPosition = targetNode.positionAbsolute;

  const w = intersectionNodeWidth / 2;
  const h = intersectionNodeHeight / 2;

  const x2 = intersectionNodePosition.x + w;
  const y2 = intersectionNodePosition.y + h;
  const x1 = targetPosition.x + targetNode.width / 2;
  const y1 = targetPosition.y + targetNode.height / 2;

  const xx1 = (x1 - x2) / (2 * w) - (y1 - y2) / (2 * h);
  const yy1 = (x1 - x2) / (2 * w) + (y1 - y2) / (2 * h);
  const a = 1 / (Math.abs(xx1) + Math.abs(yy1));
  const xx3 = a * xx1;
  const yy3 = a * yy1;
  const x = w * (xx3 + yy3) + x2;
  const y = h * (-xx3 + yy3) + y2;

  return { x, y };
}

// returns the position (top,right,bottom or right) passed node compared to the intersection point
function getEdgePosition(node: any, intersectionPoint: any) {
  const n = { ...node.positionAbsolute, ...node };
  const nx = Math.round(n.x);
  const ny = Math.round(n.y);
  const px = Math.round(intersectionPoint.x);
  const py = Math.round(intersectionPoint.y);

  if (px <= nx + 1) {
    return Position.Left;
  }
  if (px >= nx + n.width - 1) {
    return Position.Right;
  }
  if (py <= ny + 1) {
    return Position.Top;
  }
  if (py >= n.y + n.height - 1) {
    return Position.Bottom;
  }

  return Position.Top;
}

// returns the parameters (sx, sy, tx, ty, sourcePos, targetPos) you need to create an edge
export function getEdgeParams(source: any, target: any) {
  const sourceIntersectionPoint = getNodeIntersection(source, target);
  const targetIntersectionPoint = getNodeIntersection(target, source);

  const sourcePos = getEdgePosition(source, sourceIntersectionPoint);
  const targetPos = getEdgePosition(target, targetIntersectionPoint);

  return {
    sx: sourceIntersectionPoint.x,
    sy: sourceIntersectionPoint.y,
    tx: targetIntersectionPoint.x,
    ty: targetIntersectionPoint.y,
    sourcePos,
    targetPos,
  };
}

const actionVerbs = [
  'Get',
  'List',
  'Put',
  'Delete',
  'Publish',
  'Detail',
  'Manage',
];

function verbFromNitricAction(action: string) {
  for (const verb of actionVerbs) {
    if (action.endsWith(verb)) {
      return verb
    }
  }

  return action
}

export function generateVisualizerData(data: WebSocketResponse): {
  nodes: Node[]
  edges: Edge[]
} {
  const nodes: Node[] = []
  const edges: Edge[] = []

  console.log('data:', data)

  // Generate nodes from APIs
  data.apis.forEach((api) => {
    const routes = (api.spec && Object.keys(api.spec.paths)) || []

    const node = createNode<ApiNodeData>(api, 'api', {
      title: api.name,
      resource: api,
      icon: GlobeAltIcon,
      description: `${routes.length} ${
        routes.length === 1 ? 'Route' : 'Routes'
      }`,
    })

    const specEntries = (api.spec && api.spec.paths) || []

    Object.entries(specEntries).forEach(([path, operations]) => {
      AllHttpMethods.forEach((m) => {
        const method = operations && (operations[m] as any)

        if (!method) {
          return
        }

        edges.push({
          id: `e-${api.name}-${method['x-nitric-target']['name']}`,
          source: node.id,
          target: method['x-nitric-target']['name'],
          animated: true,
          markerEnd: {
            type: MarkerType.ArrowClosed,
          },
          markerStart: {
            type: MarkerType.ArrowClosed,
            orient: 'auto-start-reverse',
          },
          label: "routes",
        })
      })
    })

    nodes.push(node)
  })

  // Generate nodes from websockets
  data.websockets.forEach((ws) => {
    const node = createNode<WebsocketNodeData>(ws, 'websocket', {
      title: ws.name,
      resource: ws,
      icon: ChatBubbleLeftRightIcon,
      description: `${ws.events.length} ${
        ws.events.length === 1 ? 'Event' : 'Events'
      }`,
    })

    edges.push(
      ...Object.entries(ws.targets).map(([eventType, target]) => {
        return {
          id: `e-${ws.name}-${target}-${eventType}`,
          source: node.id,
          target,
          animated: true,
          markerEnd: {
            type: MarkerType.ArrowClosed,
          },
          markerStart: {
            type: MarkerType.ArrowClosed,
            orient: 'auto-start-reverse',
          },
          label: eventType,
        }
      }),
    )

    nodes.push(node)
  })

  // Generate nodes from schedules
  data.schedules.forEach((schedule) => {
    const node = createNode<ScheduleNodeData>(schedule, 'schedule', {
      title: schedule.name,
      resource: schedule,
      icon: ClockIcon,
      description: ``,
    })

    nodes.push(node)

    edges.push({
      id: `e-${schedule.name}-${schedule.target}`,
      source: node.id,
      target: schedule.target,
      animated: true,
      markerEnd: {
        type: MarkerType.ArrowClosed,
      },
      markerStart: {
        type: MarkerType.ArrowClosed,
        orient: 'auto-start-reverse',
      },
      label: 'Triggers',
    })
  })

  data.stores.forEach((store) => {
    const node = createNode<BucketNodeData>(store, 'keyvaluestore', {
      title: store.name,
      resource: store,
      icon: CircleStackIcon,
    })

    nodes.push(node)
  })

  // Generate nodes from buckets
  data.buckets.forEach((bucket) => {
    const bucketNotifications = getBucketNotifications(
      bucket,
      data.notifications,
    )
    const node = createNode<BucketNodeData>(bucket, 'bucket', {
      title: bucket.name,
      resource: bucket,
      icon: ArchiveBoxIcon,
      description: `${bucketNotifications.length} ${
        bucketNotifications.length === 1 ? 'Notification' : 'Notifications'
      }`,
    })

    edges.push(
      ...bucketNotifications.map((notify) => {
        return {
          id: `e-${notify.bucket}-${notify.target}`,
          source: `bucket-${notify.bucket}`,
          target: notify.target,
          animated: true,
          markerEnd: {
            type: MarkerType.ArrowClosed,
          },
          markerStart: {
            type: MarkerType.ArrowClosed,
            orient: 'auto-start-reverse',
          },
          label: 'Triggers',
        }
      }),
    )

    nodes.push(node)
  })

  // Generate nodes from buckets
  data.topics.forEach((topic) => {
    const node = createNode<TopicNodeData>(topic, 'topic', {
      title: topic.name,
      resource: topic,
      icon: MegaphoneIcon,
      description: ``,
    })
    nodes.push(node)

    const topicSubscriptions = data.subscriptions.filter(
      (sub) => sub.topic === topic.name,
    )

    edges.push(
      ...topicSubscriptions.map((subscription) => {
        return {
          id: `e-${subscription.topic}-${subscription.target}`,
          source: node.id,
          target: subscription.target,
          animated: true,
          markerEnd: {
            type: MarkerType.ArrowClosed,
          },
          markerStart: {
            type: MarkerType.ArrowClosed,
            orient: 'auto-start-reverse',
          },
          label: 'Triggers',
        }
      }),
    )
  })

  edges.push(
    ...Object.entries(data.policies).map(([_, policy]) => {
      return {
        id: `e-${policy.name}`,
        source: policy.principals[0].name,
        target: `${policy.resources[0].type}-${policy.resources[0].name}`,
        markerEnd: {
          type: MarkerType.ArrowClosed,
        },
        markerStart: {
          type: MarkerType.ArrowClosed,
          orient: 'auto-start-reverse',
        },
        label: policy.actions.map(verbFromNitricAction).join(', '),
      } as Edge
    }),
  )

  data.services.forEach((service) => {
    const node: Node<ServiceNodeData> = {
      id: service.name,
      position: { x: 0, y: 0 },
      data: {
        title: `${service.name.replace(/\\/g, '/')}`,
        description: '',
        resource: {
          filePath: service.filePath,
        },
        icon: CpuChipIcon,
      },
      type: 'service',
    }
    nodes.push(node)
  })

  console.log('nodes:', nodes)
  console.log('edges:', edges)

  return { nodes, edges }
}
