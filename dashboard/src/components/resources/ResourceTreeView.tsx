import { FC, useMemo } from "react";
import type { WorkerResource } from "../../types";
import TreeView, { TreeItemType } from "../shared/TreeView";
import type { TreeItem, TreeItemIndex } from "react-complex-tree";
import type { Resource } from "./utils";

export type ResourceTreeItemType = TreeItemType<Resource>;

interface Props {
  resources: Resource[];
  onSelect: (resource: Resource) => void;
  initialItem: Resource;
}

const ResourceTreeView: FC<Props> = ({ resources, onSelect, initialItem }) => {
  const treeItems: Record<
    TreeItemIndex,
    TreeItem<ResourceTreeItemType>
  > = useMemo(() => {
    const rootItem: TreeItem = {
      index: "root",
      isFolder: true,
      children: [],
      data: null,
    };

    const rootItems: Record<TreeItemIndex, TreeItem<ResourceTreeItemType>> = {
      root: rootItem,
    };

    for (const resource of resources) {
      // add api if not added already
      if (!rootItems[resource.name]) {
        rootItems[resource.name] = {
          index: resource.name,
          data: {
            label: resource.name,
            data: resource,
          },
        };

        rootItem.children!.push(resource.name);
      }
    }

    return rootItems;
  }, [resources]);

  return (
    <TreeView<ResourceTreeItemType>
      label="Resources"
      items={treeItems}
      initialItem={initialItem.name}
      getItemTitle={(item) => item.data.label}
      onPrimaryAction={(items) => {
        if (items.data.data) {
          onSelect(items.data.data);
        }
      }}
      renderItemTitle={({ item }) => (
        <span className="truncate">{item.data.label}</span>
      )}
    />
  );
};

export default ResourceTreeView;
