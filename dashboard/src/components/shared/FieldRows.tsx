import { XMarkIcon } from "@heroicons/react/20/solid";
import classNames from "classnames";
import React, { useEffect, useId } from "react";

export interface FieldRow {
  key: string;
  value: string;
}

interface Props {
  testId: string;
  rows: FieldRow[];
  required?: boolean;
  lockKeys?: boolean;
  setRows: (value: FieldRow[]) => void;
}

const FieldRows: React.FC<Props> = ({
  testId,
  rows,
  lockKeys,
  setRows,
  required = false,
}) => {
  const id = useId();

  useEffect(() => {
    if (
      !lockKeys &&
      (rows[rows.length - 1].key || rows[rows.length - 1].value)
    ) {
      setRows([
        ...rows,
        {
          key: "",
          value: "",
        },
      ]);
    }
  }, [rows]);

  return (
    <ul className="divide-y divide-gray-200">
      {rows.map((r, i) => {
        const keyId = `${id}-${i}-key`;
        const valueId = `${id}-${i}-value`;

        return (
          <li
            key={i}
            className="grid relative group items-center grid-cols-2 gap-4 py-4"
          >
            <div>
              <label htmlFor={keyId} className="sr-only">
                Key
              </label>
              <div className="mt-2 sm:col-span-2 sm:mt-0">
                <input
                  type="text"
                  data-testid={`${testId}-${i}-key`}
                  readOnly={lockKeys}
                  placeholder="Key"
                  onChange={(e) => {
                    const updatedRow: FieldRow = { ...r, key: e.target.value };
                    const newArr = [...rows];

                    newArr[i] = updatedRow;

                    setRows(newArr);
                  }}
                  value={r.key}
                  name={keyId}
                  id={keyId}
                  className="block w-full px-2 rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-blue-600 sm:text-sm sm:leading-6"
                />
              </div>
            </div>
            <div className="pr-8">
              <label htmlFor={valueId} className="sr-only">
                {r.value}
              </label>
              <div className="mt-2 sm:col-span-2 sm:mt-0">
                <input
                  type="text"
                  placeholder="Value"
                  data-testid={`${testId}-${i}-value`}
                  onChange={(e) => {
                    const updatedRow: FieldRow = {
                      ...r,
                      value: e.target.value,
                    };
                    const newArr = [...rows];

                    newArr[i] = updatedRow;

                    setRows(newArr);
                  }}
                  name={valueId}
                  id={valueId}
                  value={r.value}
                  className={classNames(
                    "block w-full px-2 rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset sm:text-sm sm:leading-6",
                    required && !r.value
                      ? "ring-red-700"
                      : "focus:ring-blue-600"
                  )}
                />
              </div>
            </div>
            <button
              type="button"
              onClick={() => {
                const newArray = [...rows];
                newArray.splice(i, 1);
                setRows(newArray);
              }}
              className={classNames(
                "rounded-full hidden absolute right-0 bg-gray-600 p-1 text-white shadow-sm hover:bg-blue-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600",
                rows.length > 1 && (r.key || r.value) ? "group-hover:block" : ""
              )}
            >
              <XMarkIcon className="h-5 w-5" aria-hidden="true" />
            </button>
          </li>
        );
      })}
    </ul>
  );
};

export default FieldRows;
