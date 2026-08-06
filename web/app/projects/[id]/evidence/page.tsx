import { Workspace } from "../../../page";

type ProjectPageProps = { params: Promise<{ id: string }> };

export default async function ProjectEvidencePage({ params }: ProjectPageProps) {
  const { id } = await params;
  return <Workspace initialSection="evidence" initialProjectID={id} />;
}
