import ApexCharts from "apexcharts";

// ===== chartOne
const chart23 = () => {
  const growthOptions = {
    chart: {
      type: "bar",
      height: 256,
      toolbar: { show: false },
      fontFamily: "Outfit, sans-serif",
    },
    plotOptions: {
      bar: {
        horizontal: false,
        columnWidth: "50%",
        endingShape: "rounded",
        borderRadius: 4,
        borderRadiusApplication: "end",
      },
    },
    dataLabels: {
      enabled: false,
    },
    stroke: {
      show: true,
      width: 4,
      colors: ["transparent"],
    },
    colors: ["#C2D6FF", "#465FFF"], // Light and dark blue
    series: [
      {
        name: "2023",
        data: [80, 60, 70, 40, 65, 45, 48, 55, 58, 50, 67, 75],
      },
      {
        name: "2024",
        data: [90, 50, 65, 25, 78, 68, 75, 90, 30, 70, 90, 95],
      },
    ],
    xaxis: {
      categories: [
        "Jan",
        "Feb",
        "Mar",
        "Apr",
        "May",
        "Jun",
        "Jul",
        "Aug",
        "Sep",
        "Oct",
        "Nov",
        "Dec",
      ],
      axisTicks: {
        show: false,
      },
      axisBorder: {
        show: false,
      },
    },
    yaxis: {
      labels: {
        formatter: (val) => `${val}%`,
        style: {
          fontSize: "12px",
          colors: "#344054",
        },
      },
      max: 100,
    },
    fill: {
      opacity: 1,
    },
    tooltip: {
      y: {
        formatter: (val) => `${val}%`,
      },
    },
    legend: {
      show: false,
    },
    grid: {
      borderColor: "#F2F4F7",
      strokeDashArray: 0,
    },
  };

  const chartSelector = document.querySelectorAll("#chartTwentyThree");

  if (chartSelector.length) {
    const chartTwentyThree = new ApexCharts(
      document.querySelector("#chartTwentyThree"),
      growthOptions,
    );
    chartTwentyThree.render();
  }
};

export default chart23;
