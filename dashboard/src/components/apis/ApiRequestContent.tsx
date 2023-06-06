import { formatJSON, getFileExtension } from "../../lib/utils";
import CodeEditor from "./CodeEditor";

interface Props {
  data: string;
  headers: Record<string, string[]>;
}

const APIRequestContent: React.FC<Props> = ({ data, headers }) => {
  const contentTypes = Object.entries(headers).find(
    ([key, _]) => key.toLowerCase() === "content-type"
  );

  if (!contentTypes) return null;

  const contentType = contentTypes[1][0];

  if (contentType.startsWith("image/")) {
    return (
      <img
        data-testid="response-image"
        src={data}
        alt={"response content"}
        className="w-full max-h-96 object-contain"
      />
    );
  } else if (contentType.startsWith("video/")) {
    return <video src={data} controls />;
  } else if (contentType.startsWith("audio/")) {
    return <audio src={data} controls />;
  } else if (contentType === "application/pdf") {
    return <iframe title="Response PDF" className="h-96" src={data} />;
  } else if (
    contentType.startsWith("application/") &&
    contentType !== "application/json"
  ) {
    const ext = getFileExtension(contentType);

    const fileName = data.split("/")[3] + ext;

    return (
      <div className="my-4">
        The response is binary, you can{" "}
        <a
          href={data}
          data-testid="response-binary-link"
          className="underline"
          download={fileName}
        >
          download the file here
        </a>
        .
      </div>
    );
  }

  if (contentType === "application/json") {
    // format
    data = formatJSON(data);
  }

  return (
    <CodeEditor
      id="api-response"
      enableCopy
      contentType={contentType}
      value={data}
      readOnly
    />
  );
};

export default APIRequestContent;
